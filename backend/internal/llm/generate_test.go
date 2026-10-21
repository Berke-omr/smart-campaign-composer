package llm_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

var cities = []string{
	"İstanbul", "Ankara", "İzmir", "Bursa", "Antalya", "Adana", "Konya", "Gaziantep",
	"Kocaeli", "Eskişehir", "Mersin", "Kayseri", "Samsun", "Trabzon", "Diyarbakır",
}

var longest = content.Values{
	content.FirstName: "Muhammed Emin", content.LastName: "Montgomery-Fitzgerald", content.City: "İstanbul",
}

// recording wraps a client and keeps every request it sends.
type recording struct {
	llm.Client
	mu   sync.Mutex
	reqs []llm.Request
}

func (r *recording) Complete(ctx context.Context, req llm.Request, onText func(string)) (llm.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.Client.Complete(ctx, req, onText)
}

func (r *recording) requests(task llm.Task) []llm.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []llm.Request
	for _, q := range r.reqs {
		if q.Task == task {
			out = append(out, q)
		}
	}
	return out
}

func setup(t *testing.T, opts mock.Options) (*mock.Catalog, *recording, *llm.Generator) {
	t.Helper()
	cat, err := mock.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	opts.Cities = cities
	rec := &recording{Client: mock.New(cat, opts)}
	return cat, rec, &llm.Generator{Client: rec, AttemptTimeout: 50 * time.Millisecond}
}

func scenario(t *testing.T, cat *mock.Catalog, id string) *mock.Scenario {
	t.Helper()
	s, ok := cat.Scenario(id)
	if !ok {
		t.Fatalf("no scenario %s", id)
	}
	return s
}

func input(s *mock.Scenario) llm.DraftInput {
	lv := map[content.Channel]content.Values{}
	for _, ch := range s.Channels {
		lv[ch] = longest
	}
	return llm.DraftInput{Brief: s.Brief, Tone: s.Tone, CampaignName: "Test", Channels: s.Channels, Longest: lv}
}

type collector struct {
	mu     sync.Mutex
	events []llm.Event
}

func (c *collector) emit(e llm.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

// states renders a channel's state events as "writing1 validating1 ...".
func (c *collector) states(ch content.Channel) string {
	var s []string
	for _, e := range c.events {
		if e.Channel == ch && e.Type == llm.EventState {
			s = append(s, fmt.Sprintf("%s%d", e.State, e.Attempt))
		}
	}
	return strings.Join(s, " ")
}

// TestFaultScenarios is the PLAN.md P3 requirement: one test per fault
// scenario, each pinned to the exact sequence of states the UI will show.
func TestFaultScenarios(t *testing.T) {
	tests := []struct {
		id     string
		states map[content.Channel]string
	}{
		{"cart-reminder-istanbul", map[content.Channel]string{
			content.SMS:   "writing1 validating1 valid1",
			content.Email: "writing1 validating1 valid1",
		}},
		{"fault-broken-json", map[content.Channel]string{
			content.SMS:   "writing1 validating1 fixing2 validating2 valid2",
			content.Email: "writing1 validating1 valid1",
		}},
		{"fault-long-sms", map[content.Channel]string{
			content.SMS:   "writing1 validating1 fixing2 validating2 fixing3 validating3 needs_manual_edit3",
			content.Email: "writing1 validating1 valid1",
		}},
		{"fault-unknown-variable", map[content.Channel]string{
			content.Email: "writing1 validating1 fixing2 validating2 valid2",
		}},
		{"fault-refusal", map[content.Channel]string{
			// Declined twice: final after the retry. Declined once: recovered.
			content.SMS:   "writing1 fixing2 needs_manual_edit2",
			content.Email: "writing1 fixing2 validating2 valid2",
		}},
		{"fault-timeout", map[content.Channel]string{
			content.SMS:   "writing1 validating1 valid1",
			content.Email: "writing1 fixing2 fixing3 needs_manual_edit3",
		}},
		{"fault-emoji", map[content.Channel]string{
			content.SMS: "writing1 validating1 fixing2 validating2 valid2",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			cat, _, gen := setup(t, mock.Options{})
			s := scenario(t, cat, tt.id)
			var c collector
			results := gen.Drafts(context.Background(), input(s), c.emit)
			for ch, want := range tt.states {
				if got := c.states(ch); got != want {
					t.Errorf("%s states:\n got %s\nwant %s", ch, got, want)
				}
				// The final state agrees with the scenario's declared outcome.
				r := results[ch]
				if got := fmt.Sprintf("valid@%d", r.Attempts); r.State == llm.StateValid && got != s.Expect[string(ch)] ||
					r.State == llm.StateNeedsManualEdit && s.Expect[string(ch)] != "needs_manual_edit" {
					t.Errorf("%s: result %s after %d attempts, scenario expects %s", ch, r.State, r.Attempts, s.Expect[string(ch)])
				}
			}
		})
	}
}

// TestEmojiRepair: the repair request quotes the emoji the validator found,
// and the stored answer has none.
func TestEmojiRepair(t *testing.T) {
	cat, rec, gen := setup(t, mock.Options{})
	s := scenario(t, cat, "fault-emoji")
	results := gen.Drafts(context.Background(), input(s), func(llm.Event) {})
	reqs := rec.requests(llm.Task(content.SMS))
	if len(reqs) != 2 || !strings.Contains(reqs[1].Prompt, `text contains emoji ("😊")`) {
		t.Fatalf("%d SMS requests; the repair does not quote the emoji:\n%s", len(reqs), reqs[len(reqs)-1].Prompt)
	}
	if !strings.Contains(reqs[0].System, "no emoji") {
		t.Error("the system prompt does not forbid emoji")
	}
	r := results[content.SMS]
	if r.State != llm.StateValid || strings.ContainsRune(r.Message.Text, '😊') {
		t.Errorf("result = %s %q", r.State, r.Message.Text)
	}
}

func TestSegmentRepairs(t *testing.T) {
	cat, rec, gen := setup(t, mock.Options{})
	reg := segment.NewRegistry(cities)

	// Unknown field on attempt 1: the repair prompt quotes the validator.
	s := scenario(t, cat, "fault-unknown-field")
	res, err := gen.Segment(context.Background(), reg, s.Brief)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if res.Attempts != 2 || res.Validated.Len() != 1 || len(res.UnsupportedCriteria) != 1 {
		t.Errorf("result = %d attempts, %d conditions, unsupported %q", res.Attempts, res.Validated.Len(), res.UnsupportedCriteria)
	}
	reqs := rec.requests(llm.TaskSegment)
	if len(reqs) != 2 || reqs[1].Attempt != 2 {
		t.Fatalf("%d segment requests", len(reqs))
	}
	if !strings.Contains(reqs[1].Prompt, `unknown field "gender"`) || !strings.Contains(reqs[1].Prompt, "<previous_answer>") {
		t.Errorf("repair prompt lacks the problem or the previous answer:\n%s", reqs[1].Prompt)
	}
	if strings.Contains(reqs[0].Prompt, "<previous_answer>") {
		t.Error("first attempt carries a repair section")
	}

	// Every example brief gives a valid rule on the first attempt.
	for _, ex := range cat.Examples() {
		res, err := gen.Segment(context.Background(), reg, ex.Brief)
		if err != nil || res.Attempts != 1 || res.CampaignName != ex.Title {
			t.Errorf("%s: %v, %d attempts, name %q", ex.ID, err, res.Attempts, res.CampaignName)
		}
	}
}

// failing always returns the same error.
type failing struct{ err error }

func (f failing) Complete(context.Context, llm.Request, func(string)) (llm.Response, error) {
	return llm.Response{}, f.err
}

func TestSegmentGivesUp(t *testing.T) {
	reg := segment.NewRegistry(cities)
	tests := []struct {
		name         string
		err          error
		wantAttempts int
	}{
		{"refused twice", &llm.RefusalError{Category: "general_harms"}, 2},
		{"cut off every time", llm.ErrTruncated, 3},
		{"unreachable", errors.New("connection refused"), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := &llm.Generator{Client: failing{tt.err}}
			_, err := gen.Segment(context.Background(), reg, "any brief")
			var se *llm.SegmentError
			if !errors.As(err, &se) || se.Attempts != tt.wantAttempts || len(se.Problems) == 0 {
				t.Fatalf("err = %v, want SegmentError after %d attempts", err, tt.wantAttempts)
			}
		})
	}
}

func TestRepairPromptCarriesTheLengthProblem(t *testing.T) {
	cat, rec, gen := setup(t, mock.Options{})
	s := scenario(t, cat, "vip-thanks-izmir")
	var c collector
	results := gen.Drafts(context.Background(), input(s), c.emit)

	reqs := rec.requests(llm.TaskSMS)
	if len(reqs) != 2 {
		t.Fatalf("%d SMS requests, want 2", len(reqs))
	}
	if !strings.Contains(reqs[1].Prompt, "the limit is 160. Shorten it by at least") {
		t.Errorf("repair prompt:\n%s", reqs[1].Prompt)
	}
	// The fixing event tells the UI what is being fixed.
	for _, e := range c.events {
		if e.Channel == content.SMS && e.State == llm.StateFixing {
			if len(e.Problems) == 0 || !strings.Contains(e.Problems[0], "limit is 160") {
				t.Errorf("fixing event problems = %q", e.Problems)
			}
		}
	}
	if r := results[content.SMS]; r.State != llm.StateValid || !r.Report.Valid || r.Report.Lengths[0].WorstCaseLength > 160 {
		t.Errorf("SMS result = %+v", r)
	}
}

func TestNeverSilentlyTruncates(t *testing.T) {
	cat, _, gen := setup(t, mock.Options{})
	s := scenario(t, cat, "fault-long-sms")
	r := gen.Drafts(context.Background(), input(s), func(llm.Event) {})[content.SMS]

	scripted, _ := llm.ParseDraft(content.SMS, s.SMS[0].Text())
	if r.State != llm.StateNeedsManualEdit || r.Message == nil || r.Message.Text != scripted.Text {
		t.Fatalf("result = %+v; the full over-long text must be kept for the marketer", r)
	}
	if r.Report == nil || r.Report.Valid || r.Report.Errors[0].Code != content.CodeTooLong {
		t.Errorf("report = %+v", r.Report)
	}
	if len(r.Problems) == 0 || !strings.Contains(r.Problems[0], "Shorten it by at least") {
		t.Errorf("problems = %q", r.Problems)
	}
}

func TestDeltasGrowAndEndAtTheDraft(t *testing.T) {
	cat, _, gen := setup(t, mock.Options{ChunkRunes: 3})
	s := scenario(t, cat, "cart-reminder-istanbul")
	var c collector
	results := gen.Drafts(context.Background(), input(s), c.emit)

	last := map[string]string{}
	for _, e := range c.events {
		if e.Type != llm.EventDelta {
			continue
		}
		key := string(e.Channel) + "." + e.Field
		if !strings.HasPrefix(e.Value, last[key]) {
			t.Fatalf("%s went from %q to %q", key, last[key], e.Value)
		}
		last[key] = e.Value
	}
	email := results[content.Email].Message
	if last["sms.text"] != results[content.SMS].Message.Text || last["email.subject"] != email.Subject || last["email.body"] != email.Body {
		t.Errorf("last deltas %q do not match the drafts", last)
	}
	// The subject is complete before the body starts streaming.
	firstBody := -1
	for i, e := range c.events {
		if e.Field == "body" && firstBody < 0 {
			firstBody = i
		}
		if e.Field == "subject" && firstBody >= 0 {
			t.Fatal("subject delta after the body started")
		}
	}
}

func TestSMSIsValidatedWhileEmailStreams(t *testing.T) {
	cat, _, gen := setup(t, mock.Options{ChunkDelay: time.Millisecond, ChunkRunes: 2})
	gen.AttemptTimeout = 0
	s := scenario(t, cat, "cart-reminder-istanbul")
	var c collector
	gen.Drafts(context.Background(), input(s), c.emit)

	smsValid, lastEmailDelta := -1, -1
	for i, e := range c.events {
		if e.Channel == content.SMS && e.State == llm.StateValid {
			smsValid = i
		}
		if e.Channel == content.Email && e.Type == llm.EventDelta {
			lastEmailDelta = i
		}
	}
	if smsValid < 0 || smsValid > lastEmailDelta {
		t.Errorf("SMS valid at event %d, last email delta at %d: SMS should finish while the email streams", smsValid, lastEmailDelta)
	}
}

func TestDraftsStopWhenTheClientLeaves(t *testing.T) {
	cat, _, gen := setup(t, mock.Options{ChunkDelay: 5 * time.Millisecond, ChunkRunes: 1})
	gen.AttemptTimeout = 0
	s := scenario(t, cat, "cart-reminder-istanbul")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	results := gen.Drafts(ctx, input(s), func(llm.Event) {})
	if time.Since(start) > time.Second {
		t.Fatalf("Drafts took %v after the context ended", time.Since(start))
	}
	for ch, r := range results {
		if r.State != llm.StateNeedsManualEdit || r.Attempts != 1 {
			t.Errorf("%s = %s after %d attempts; a cancelled draft must not be retried", ch, r.State, r.Attempts)
		}
	}
}

// observed records the attempts the generator reports for metrics.
type observed struct {
	mu    sync.Mutex
	stats []llm.AttemptStat
}

func (o *observed) observe(a llm.AttemptStat) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stats = append(o.stats, a)
}

// of renders one task's attempts as "1:invalid 2:valid".
func (o *observed) of(task llm.Task) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var s []string
	for _, a := range o.stats {
		if a.Task == task {
			s = append(s, fmt.Sprintf("%d:%s", a.Attempt, a.Result))
			if a.Duration <= 0 {
				s = append(s, "(no duration)")
			}
		}
	}
	return strings.Join(s, " ")
}

// TestObserveReportsEveryAttempt: the metrics hook hears about each attempt
// with what became of it, repairs included.
func TestObserveReportsEveryAttempt(t *testing.T) {
	tests := []struct {
		id   string
		want map[llm.Task]string
	}{
		{"cart-reminder-istanbul", map[llm.Task]string{llm.TaskSMS: "1:valid", llm.TaskEmail: "1:valid"}},
		{"fault-broken-json", map[llm.Task]string{llm.TaskSMS: "1:invalid 2:valid", llm.TaskEmail: "1:valid"}},
		{"fault-long-sms", map[llm.Task]string{llm.TaskSMS: "1:invalid 2:invalid 3:invalid"}},
		{"fault-refusal", map[llm.Task]string{llm.TaskSMS: "1:refused 2:refused", llm.TaskEmail: "1:refused 2:valid"}},
		{"fault-timeout", map[llm.Task]string{llm.TaskSMS: "1:valid", llm.TaskEmail: "1:timeout 2:timeout 3:timeout"}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			cat, _, gen := setup(t, mock.Options{})
			var o observed
			gen.Observe = o.observe
			gen.Drafts(context.Background(), input(scenario(t, cat, tt.id)), func(llm.Event) {})
			for task, want := range tt.want {
				if got := o.of(task); got != want {
					t.Errorf("%s: %q, want %q", task, got, want)
				}
			}
		})
	}

	t.Run("segment", func(t *testing.T) {
		cat, _, gen := setup(t, mock.Options{})
		var o observed
		gen.Observe = o.observe
		if _, err := gen.Segment(context.Background(), segment.NewRegistry(cities), scenario(t, cat, "fault-unknown-field").Brief); err != nil {
			t.Fatal(err)
		}
		if got := o.of(llm.TaskSegment); got != "1:invalid 2:valid" {
			t.Errorf("segment: %q, want 1:invalid 2:valid", got)
		}
		for _, f := range []struct {
			err  error
			want string
		}{
			{llm.ErrTruncated, "1:truncated 2:truncated 3:truncated"},
			{errors.New("connection refused"), "1:error 2:error 3:error"},
		} {
			var o observed
			gen := &llm.Generator{Client: failing{f.err}, Observe: o.observe}
			_, _ = gen.Segment(context.Background(), segment.NewRegistry(cities), "any brief")
			if got := o.of(llm.TaskSegment); got != f.want {
				t.Errorf("%v: %q, want %q", f.err, got, f.want)
			}
		}
	})

	// An attempt cut short because the caller left is not the model's
	// doing and is not reported.
	t.Run("caller left", func(t *testing.T) {
		cat, _, gen := setup(t, mock.Options{ChunkDelay: 5 * time.Millisecond, ChunkRunes: 1})
		gen.AttemptTimeout = 0
		var o observed
		gen.Observe = o.observe
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		gen.Drafts(ctx, input(scenario(t, cat, "cart-reminder-istanbul")), func(llm.Event) {})
		if len(o.stats) != 0 {
			t.Errorf("reported %+v after the caller left", o.stats)
		}
	})
}
