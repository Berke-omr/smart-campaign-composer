package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// MaxAttempts is the attempt budget per task: one try and two repairs
// (DECISIONS.md D10, D22).
const MaxAttempts = 3

// Generator runs the model with the validation and repair loop around it.
type Generator struct {
	Client Client
	// AttemptTimeout bounds one attempt; a timeout counts as a failed attempt.
	AttemptTimeout time.Duration
	// Observe, when set, is told about every attempt that came to an end
	// (metrics). An attempt cut short because the caller left is not
	// reported: that says nothing about the model.
	Observe func(AttemptStat)
}

// AttemptStat is one attempt as the metrics see it.
type AttemptStat struct {
	Task    Task
	Attempt int // 1 is the first try; 2 and 3 are repairs
	// Duration covers the model call (streaming included) and the checks.
	Duration time.Duration
	// Result is valid, invalid (the validator rejected the answer),
	// refused, truncated, timeout or error (the model could not be reached).
	Result string
}

// attemptFailure explains a failed attempt. repair is set when the model
// produced an answer the validator rejected; it is quoted to the next attempt.
// cause is set when there was no answer to validate.
type attemptFailure struct {
	problems []string
	repair   *Repair
	refusal  bool
	cause    string // refused, truncated, timeout or error
}

// result names an attempt's outcome for AttemptStat.
func result(f *attemptFailure) string {
	switch {
	case f == nil:
		return "valid"
	case f.cause != "":
		return f.cause
	default:
		return "invalid"
	}
}

func (g *Generator) observe(ctx context.Context, task Task, attempt int, started time.Time, f *attemptFailure) {
	if g.Observe == nil || ctx.Err() != nil {
		return
	}
	g.Observe(AttemptStat{Task: task, Attempt: attempt, Duration: time.Since(started), Result: result(f)})
}

// classify turns a Client error into a failed attempt.
func classify(err error) attemptFailure {
	var refusal *RefusalError
	switch {
	case errors.As(err, &refusal):
		return attemptFailure{refusal: true, cause: "refused", problems: []string{refusal.Error()}}
	case errors.Is(err, ErrTruncated):
		return attemptFailure{cause: "truncated", problems: []string{ErrTruncated.Error()}}
	case errors.Is(err, context.DeadlineExceeded):
		return attemptFailure{cause: "timeout", problems: []string{"the model did not answer in time"}}
	default:
		return attemptFailure{cause: "error", problems: []string{"the model could not be reached"}}
	}
}

// budget tracks the attempts left. A refusal is retried once, because
// benign requests are occasionally declined (seen in the P3 smoke test); a
// second refusal is final.
type budget struct {
	attempt  int
	refusals int
}

func (b *budget) next(f attemptFailure) bool {
	if f.refusal {
		b.refusals++
		if b.refusals >= 2 {
			return false
		}
	}
	return b.attempt < MaxAttempts
}

// ---- audience (not streamed, DECISIONS.md D11) ----

// SegmentResult is a brief turned into a validated audience rule.
type SegmentResult struct {
	CampaignName        string
	Rule                json.RawMessage
	Validated           segment.Validated
	UnsupportedCriteria []string
	Attempts            int
	Usage               Usage
}

// SegmentError means no valid rule was produced within the attempt budget.
type SegmentError struct {
	Attempts int
	Problems []string
}

func (e *SegmentError) Error() string {
	return fmt.Sprintf("no valid audience rule after %d attempts: %v", e.Attempts, e.Problems)
}

// Segment turns a brief into an audience rule, repairing invalid answers.
func (g *Generator) Segment(ctx context.Context, reg *segment.Registry, brief string) (SegmentResult, error) {
	system, schema := SegmentSystem(reg), SegmentSchema(reg)
	var b budget
	var repair *Repair
	var usage Usage
	for {
		b.attempt++
		req := Request{Task: TaskSegment, Brief: brief, Attempt: b.attempt,
			System: system, Prompt: SegmentPrompt(brief, repair), Schema: schema}
		started := time.Now()
		res, f := g.segmentAttempt(ctx, reg, req)
		g.observe(ctx, TaskSegment, b.attempt, started, f)
		usage = addUsage(usage, res.Usage)
		if f == nil {
			res.Attempts, res.Usage = b.attempt, usage
			return res, nil
		}
		if ctx.Err() != nil {
			return SegmentResult{}, ctx.Err()
		}
		if !b.next(*f) {
			return SegmentResult{}, &SegmentError{Attempts: b.attempt, Problems: f.problems}
		}
		repair = f.repair
	}
}

func (g *Generator) segmentAttempt(ctx context.Context, reg *segment.Registry, req Request) (SegmentResult, *attemptFailure) {
	actx, cancel := g.attemptContext(ctx)
	defer cancel()
	resp, err := g.Client.Complete(actx, req, nil)
	if err != nil {
		f := classify(err)
		return SegmentResult{}, &f
	}
	out := SegmentResult{Usage: resp.Usage}
	p, err := ParseSegment(resp.Text)
	if err != nil {
		return out, &attemptFailure{problems: problemsOf(err), repair: &Repair{Previous: resp.Text, Problems: problemsOf(err)}}
	}
	v, err := segment.ParseAndValidate(p.Rule, reg)
	if err != nil {
		return out, &attemptFailure{problems: problemsOf(err), repair: &Repair{Previous: resp.Text, Problems: problemsOf(err)}}
	}
	out.CampaignName, out.Rule, out.Validated, out.UnsupportedCriteria = p.CampaignName, p.Rule, v, p.UnsupportedCriteria
	return out, nil
}

// ---- drafts (streamed per channel) ----

// State is a channel draft's progress, as shown on its card (D22).
type State string

const (
	StateWriting         State = "writing"
	StateValidating      State = "validating"
	StateFixing          State = "fixing"
	StateValid           State = "valid"
	StateNeedsManualEdit State = "needs_manual_edit"
)

// Terminal reports whether no more events follow for the channel.
func (s State) Terminal() bool { return s == StateValid || s == StateNeedsManualEdit }

// Event types sent to the client (docs/API.md).
const (
	EventState = "draft.state"
	EventDelta = "draft.delta"
)

// Event is one step of a channel's draft.
type Event struct {
	Type    string          `json:"-"`
	Channel content.Channel `json:"channel"`
	Attempt int             `json:"attempt"`

	// draft.state
	State       State    `json:"state,omitempty"`
	MaxAttempts int      `json:"max_attempts,omitempty"`
	Problems    []string `json:"problems,omitempty"` // what the fixing attempt corrects
	// Result is set when State is terminal.
	Result *ChannelResult `json:"result,omitempty"`

	// draft.delta: the current value of one field, decoded so far. Each
	// delta replaces the previous value of that field.
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
}

// ChannelResult is a channel's final draft.
type ChannelResult struct {
	Channel  content.Channel `json:"channel"`
	State    State           `json:"state"` // valid or needs_manual_edit
	Attempts int             `json:"attempts"`
	// Message is the last parsed draft; nil if the model never produced one.
	Message *content.Message `json:"message"`
	// Report validates Message; nil when Message is nil.
	Report *content.Report `json:"report"`
	// Problems explain needs_manual_edit; empty when valid.
	Problems []string `json:"problems"`
	Usage    Usage    `json:"usage"`
}

// DraftInput is everything the copywriter is told.
type DraftInput struct {
	Brief        string
	Tone         Tone
	CampaignName string
	Audience     []string // segment.Describe lines
	Channels     []content.Channel
	// Longest holds each channel's longest recipient values (D15).
	Longest map[content.Channel]content.Values
}

// Drafts writes every selected channel in parallel and returns their
// results. emit receives the events in order per channel; calls are
// serialized, so emit does not need to be safe for concurrent use. SMS is
// short and usually finishes first, so it is validated while the email is
// still streaming.
func (g *Generator) Drafts(ctx context.Context, in DraftInput, emit func(Event)) map[content.Channel]ChannelResult {
	var mu sync.Mutex
	safeEmit := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		emit(e)
	}
	results := make(map[content.Channel]ChannelResult, len(in.Channels))
	var wg sync.WaitGroup
	for _, ch := range in.Channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := g.draft(ctx, in, ch, safeEmit)
			mu.Lock()
			results[ch] = r
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func (g *Generator) draft(ctx context.Context, in DraftInput, ch content.Channel, emit func(Event)) ChannelResult {
	task := Task(ch)
	brief := ContentBrief{Brief: in.Brief, Tone: in.Tone, CampaignName: in.CampaignName, Audience: in.Audience, Longest: in.Longest[ch]}
	system, schema := ContentSystem(ch), ContentSchema(ch)

	res := ChannelResult{Channel: ch, Problems: []string{}}
	var b budget
	var repair *Repair
	var problems []string
	for {
		b.attempt++
		state := StateWriting
		if b.attempt > 1 {
			state = StateFixing
		}
		emit(Event{Type: EventState, Channel: ch, Attempt: b.attempt, State: state, MaxAttempts: MaxAttempts, Problems: problems})

		req := Request{Task: task, Brief: in.Brief, Attempt: b.attempt, System: system,
			Prompt: ContentPrompt(ch, brief, repair), Schema: schema}
		started := time.Now()
		f := g.draftAttempt(ctx, req, ch, in.Longest[ch], &res, emit)
		g.observe(ctx, task, b.attempt, started, f)
		res.Attempts = b.attempt
		if f == nil {
			res.State = StateValid
			emit(Event{Type: EventState, Channel: ch, Attempt: b.attempt, State: StateValid, MaxAttempts: MaxAttempts, Result: &res})
			return res
		}
		if ctx.Err() != nil || !b.next(*f) {
			res.State, res.Problems = StateNeedsManualEdit, f.problems
			emit(Event{Type: EventState, Channel: ch, Attempt: b.attempt, State: StateNeedsManualEdit, MaxAttempts: MaxAttempts, Result: &res})
			return res
		}
		repair, problems = f.repair, f.problems
	}
}

// draftAttempt streams one attempt, then validates it. On success res holds
// the draft and its report; otherwise the failure says why.
func (g *Generator) draftAttempt(ctx context.Context, req Request, ch content.Channel, longest content.Values,
	res *ChannelResult, emit func(Event)) *attemptFailure {
	actx, cancel := g.attemptContext(ctx)
	defer cancel()

	fields := []string{"text"}
	if ch == content.Email {
		fields = []string{"subject", "body"}
	}
	var text []byte
	seen := map[string]string{}
	resp, err := g.Client.Complete(actx, req, func(fragment string) {
		text = append(text, fragment...)
		current := PartialStrings(string(text))
		for _, field := range fields {
			if value, ok := current[field]; ok && seen[field] != value {
				seen[field] = value
				emit(Event{Type: EventDelta, Channel: ch, Attempt: req.Attempt, Field: field, Value: value})
			}
		}
	})
	res.Usage = addUsage(res.Usage, resp.Usage)
	if err != nil {
		f := classify(err)
		return &f
	}

	emit(Event{Type: EventState, Channel: ch, Attempt: req.Attempt, State: StateValidating, MaxAttempts: MaxAttempts})
	m, err := ParseDraft(ch, resp.Text)
	if err != nil {
		return &attemptFailure{problems: problemsOf(err), repair: &Repair{Previous: resp.Text, Problems: problemsOf(err)}}
	}
	report := content.Validate(ch, m, longest)
	res.Message, res.Report = &m, &report
	if !report.Valid {
		probs := problemsOf(report.Errors)
		return &attemptFailure{problems: probs, repair: &Repair{Previous: resp.Text, Problems: probs}}
	}
	return nil
}

func (g *Generator) attemptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if g.AttemptTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, g.AttemptTimeout)
}

// problemsOf flattens any validator's errors into messages for the repair
// prompt and the UI.
func problemsOf(err error) []string {
	var lp Problems
	var se segment.Errors
	var ce content.Errors
	switch {
	case errors.As(err, &lp):
		return append([]string(nil), lp...)
	case errors.As(err, &se):
		out := make([]string, len(se))
		for i, fe := range se {
			if fe.Index >= 0 {
				out[i] = fmt.Sprintf("condition %d: %s", fe.Index, fe.Message)
			} else {
				out[i] = fe.Message
			}
		}
		return out
	case errors.As(err, &ce):
		out := make([]string, len(ce))
		for i, fe := range ce {
			out[i] = fe.Message
		}
		return out
	}
	return []string{err.Error()}
}

func addUsage(a, b Usage) Usage {
	return Usage{
		InputTokens:      a.InputTokens + b.InputTokens,
		OutputTokens:     a.OutputTokens + b.OutputTokens,
		CacheReadTokens:  a.CacheReadTokens + b.CacheReadTokens,
		CacheWriteTokens: a.CacheWriteTokens + b.CacheWriteTokens,
	}
}
