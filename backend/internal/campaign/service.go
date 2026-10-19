package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// MaxBriefRunes bounds the brief a marketer can send.
const MaxBriefRunes = 2000

// Audience counts and measures segments.
type Audience interface {
	Count(ctx context.Context, v segment.Validated, asOf time.Time) (segment.Audience, error)
	LongestValues(ctx context.Context, v segment.Validated, asOf time.Time) (map[content.Channel]content.Values, error)
}

// Service runs the campaign draft flow.
type Service struct {
	Repo      *Repository
	Generator *llm.Generator
	Registry  *segment.Registry
	Audience  Audience
	LLMMode   string
	LLMModel  *string
	Logger    *slog.Logger
	Now       func() time.Time

	mu        sync.Mutex
	streaming map[int64]bool
}

// InputError is a request the service refuses before calling the model.
type InputError struct{ Problems []string }

func (e *InputError) Error() string { return strings.Join(e.Problems, "; ") }

// ErrBusy means drafts for this campaign are already being written.
var ErrBusy = errors.New("drafts for this campaign are already being written")

// CreateInput is what the composer sends.
type CreateInput struct {
	Brief    string            `json:"brief"`
	Channels []content.Channel `json:"channels"`
	Tone     llm.Tone          `json:"tone"`
}

func (in *CreateInput) normalize() error {
	var probs []string
	in.Brief = strings.TrimSpace(in.Brief)
	switch n := utf8.RuneCountInString(in.Brief); {
	case n == 0:
		probs = append(probs, "brief is empty")
	case n > MaxBriefRunes:
		probs = append(probs, fmt.Sprintf("brief has %d characters; the limit is %d", n, MaxBriefRunes))
	}
	var chs []content.Channel
	for _, ch := range content.Channels { // canonical order, duplicates dropped
		if slices.Contains(in.Channels, ch) {
			chs = append(chs, ch)
		}
	}
	for _, ch := range in.Channels {
		if !ch.Valid() {
			probs = append(probs, fmt.Sprintf("unknown channel %q; allowed: sms, email", ch))
		}
	}
	if len(in.Channels) == 0 {
		probs = append(probs, "select at least one channel")
	}
	in.Channels = chs
	if in.Tone == "" {
		in.Tone = llm.Tones[0]
	}
	if !in.Tone.Valid() {
		probs = append(probs, fmt.Sprintf("unknown tone %q", in.Tone))
	}
	if len(probs) > 0 {
		return &InputError{Problems: probs}
	}
	return nil
}

// Create turns a brief into a draft campaign: the model proposes the
// audience rule and a name, the backend validates the rule (with repairs),
// and the draft is stored. Messages are written by StreamDrafts.
func (s *Service) Create(ctx context.Context, in CreateInput) (View, error) {
	if err := in.normalize(); err != nil {
		return View{}, err
	}
	asOf := s.Now().UTC()
	res, err := s.Generator.Segment(ctx, s.Registry, in.Brief)
	if err != nil {
		return View{}, err
	}
	s.Logger.Info("audience rule ready", "attempts", res.Attempts,
		"input_tokens", res.Usage.InputTokens, "output_tokens", res.Usage.OutputTokens, "cache_read_tokens", res.Usage.CacheReadTokens)

	c := Campaign{
		Name: res.CampaignName, Brief: in.Brief, Tone: in.Tone, Channels: in.Channels,
		Rule: res.Rule, UnsupportedCriteria: res.UnsupportedCriteria, AsOf: asOf,
		LLMMode: s.LLMMode, LLMModel: s.LLMModel,
	}
	if err := s.Repo.Create(ctx, &c); err != nil {
		return View{}, err
	}
	return s.Get(ctx, c.ID)
}

// Get returns a campaign as the UI shows it.
func (s *Service) Get(ctx context.Context, id int64) (View, error) {
	c, err := s.Repo.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	v, longest, err := s.audience(ctx, c)
	if err != nil {
		return View{}, err
	}
	return s.view(c, v, longest), nil
}

// List returns the most recent campaigns with their audience size.
func (s *Service) List(ctx context.Context, limit int) ([]Summary, error) {
	cs, err := s.Repo.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(cs))
	for _, c := range cs {
		v, err := segment.ParseAndValidate(c.Rule, s.Registry)
		if err != nil {
			return nil, fmt.Errorf("campaign %d: stored rule no longer valid: %w", c.ID, err)
		}
		a, err := s.Audience.Count(ctx, v, c.AsOf)
		if err != nil {
			return nil, err
		}
		out = append(out, Summary{ID: c.ID, Name: c.Name, Status: c.Status, Channels: c.Channels,
			Audience: a.Reach(c.Channels), CreatedAt: c.CreatedAt})
	}
	return out, nil
}

// audience re-validates the stored rule and measures it at the campaign's
// anchor time.
func (s *Service) audience(ctx context.Context, c Campaign) (audienceView, map[content.Channel]content.Values, error) {
	v, err := segment.ParseAndValidate(c.Rule, s.Registry)
	if err != nil {
		return audienceView{}, nil, fmt.Errorf("campaign %d: stored rule no longer valid: %w", c.ID, err)
	}
	a, err := s.Audience.Count(ctx, v, c.AsOf)
	if err != nil {
		return audienceView{}, nil, err
	}
	longest, err := s.Audience.LongestValues(ctx, v, c.AsOf)
	if err != nil {
		return audienceView{}, nil, err
	}
	return audienceView{Reach: a.Reach(c.Channels), Description: segment.Describe(v)}, longest, nil
}

// Emit sends one server-sent event; an error means the client is gone.
type Emit func(event string, data any) error

// Event types besides llm.EventState and llm.EventDelta (docs/API.md).
const (
	EventDone  = "draft.done"
	EventError = "draft.error"
)

// StreamDrafts writes the messages of every selected channel that has none
// yet and streams their progress. Channels already drafted are replayed
// from the database without calling the model, so reopening a finished
// draft costs nothing. Each finished channel is stored before its final
// event is sent.
func (s *Service) StreamDrafts(ctx context.Context, id int64, emit Emit) error {
	c, err := s.Repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !c.Status.Editable() {
		return ErrLocked
	}
	if !s.begin(id) {
		return ErrBusy
	}
	defer s.end(id)

	av, longest, err := s.audience(ctx, c)
	if err != nil {
		return err
	}

	var missing []content.Channel
	for _, ch := range c.Channels {
		stored, ok := c.Messages[ch]
		if !ok {
			missing = append(missing, ch)
			continue
		}
		if err := emit(llm.EventState, storedEvent(ch, stored, longest[ch])); err != nil {
			return nil // client gone
		}
	}

	status := c.Status
	if len(missing) > 0 {
		in := llm.DraftInput{
			Brief: c.Brief, Tone: c.Tone, CampaignName: c.Name, Audience: av.Description.Lines,
			Channels: missing, Longest: longest,
		}
		gone := false
		results := s.Generator.Drafts(ctx, in, func(e llm.Event) {
			if gone {
				return
			}
			if e.Type == llm.EventState && e.State.Terminal() && ctx.Err() == nil {
				st, err := s.Repo.SaveMessage(ctx, id, e.Channel, toStored(*e.Result))
				if err != nil {
					s.Logger.Error("saving draft failed", "campaign", id, "channel", e.Channel, "err", err)
					gone = emit(EventError, map[string]string{"channel": string(e.Channel), "message": "the draft could not be saved"}) != nil
					return
				}
				status = st
			}
			if emit(e.Type, e) != nil {
				gone = true
			}
		})
		for ch, r := range results {
			s.Logger.Info("draft finished", "campaign", id, "channel", ch, "state", r.State, "attempts", r.Attempts,
				"input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens, "cache_read_tokens", r.Usage.CacheReadTokens)
		}
		if gone || ctx.Err() != nil {
			return nil
		}
	}
	_ = emit(EventDone, map[string]any{"campaign_id": id, "status": status})
	return nil
}

func (s *Service) begin(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streaming == nil {
		s.streaming = map[int64]bool{}
	}
	if s.streaming[id] {
		return false
	}
	s.streaming[id] = true
	return true
}

func (s *Service) end(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.streaming, id)
}

// ContentError is an edit the validator rejected; nothing was stored.
type ContentError struct{ Report content.Report }

func (e *ContentError) Error() string { return e.Report.Errors.Error() }

// EditMessage replaces a channel's message with the marketer's version. It
// goes through the same validator as the model's drafts (D10); an invalid
// edit is rejected and the stored message is left as it was.
func (s *Service) EditMessage(ctx context.Context, id int64, ch content.Channel, m content.Message) (MessageView, Status, error) {
	c, err := s.Repo.Get(ctx, id)
	if err != nil {
		return MessageView{}, "", err
	}
	if !slices.Contains(c.Channels, ch) {
		return MessageView{}, "", ErrNotFound
	}
	if !c.Status.Editable() {
		return MessageView{}, c.Status, ErrLocked
	}
	_, longest, err := s.audience(ctx, c)
	if err != nil {
		return MessageView{}, "", err
	}
	report := content.Validate(ch, m, longest[ch])
	if !report.Valid {
		return MessageView{}, c.Status, &ContentError{Report: report}
	}
	stored := StoredMessage{State: llm.StateValid, Message: m, Source: SourceEdited, Problems: []string{}}
	if prev, ok := c.Messages[ch]; ok {
		stored.Attempts = prev.Attempts
	}
	status, err := s.Repo.SaveMessage(ctx, id, ch, stored)
	if err != nil {
		return MessageView{}, status, err
	}
	stored.UpdatedAt = s.Now().UTC()
	return messageView(ch, stored, longest[ch]), status, nil
}

func toStored(r llm.ChannelResult) StoredMessage {
	m := StoredMessage{State: r.State, Source: SourceGenerated, Attempts: r.Attempts, Problems: r.Problems}
	if r.Message != nil {
		m.Message = *r.Message
	}
	if m.Problems == nil {
		m.Problems = []string{}
	}
	return m
}

// storedEvent replays a stored message as the final draft.state event.
func storedEvent(ch content.Channel, m StoredMessage, longest content.Values) llm.Event {
	r := llm.ChannelResult{Channel: ch, State: m.State, Attempts: m.Attempts, Problems: m.Problems}
	if hasContent(ch, m.Message) {
		msg := m.Message
		report := content.Validate(ch, msg, longest)
		r.Message, r.Report = &msg, &report
	}
	return llm.Event{Type: llm.EventState, Channel: ch, Attempt: m.Attempts, State: m.State,
		MaxAttempts: llm.MaxAttempts, Result: &r}
}

func hasContent(ch content.Channel, m content.Message) bool {
	if ch == content.Email {
		return m.Subject != "" || m.Body != ""
	}
	return m.Text != ""
}

// ---- views (the JSON the API returns, docs/API.md) ----

type audienceView struct {
	segment.Reach
	Description segment.Description `json:"description"`
}

// View is a campaign with everything the draft screen shows.
type View struct {
	ID                  int64                            `json:"id"`
	Name                string                           `json:"name"`
	Status              Status                           `json:"status"`
	Brief               string                           `json:"brief"`
	Tone                llm.Tone                         `json:"tone"`
	Channels            []content.Channel                `json:"channels"`
	Audience            audienceView                     `json:"audience"`
	Rule                json.RawMessage                  `json:"rule"`
	UnsupportedCriteria []string                         `json:"unsupported_criteria"`
	AsOf                time.Time                        `json:"as_of"`
	LLM                 llmView                          `json:"llm"`
	Messages            map[content.Channel]*MessageView `json:"messages"`
	CreatedAt           time.Time                        `json:"created_at"`
	UpdatedAt           time.Time                        `json:"updated_at"`
}

type llmView struct {
	Mode  string  `json:"mode"`
	Model *string `json:"model"`
}

// MessageView is a stored message with its validation report (lengths,
// longest values, previews), recomputed when it is read.
type MessageView struct {
	State     llm.State        `json:"state"`
	Source    string           `json:"source"`
	Attempts  int              `json:"attempts"`
	Message   *content.Message `json:"message"`
	Report    *content.Report  `json:"report"`
	Problems  []string         `json:"problems"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// Summary is a row of the campaign list.
type Summary struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Status    Status            `json:"status"`
	Channels  []content.Channel `json:"channels"`
	Audience  segment.Reach     `json:"audience"`
	CreatedAt time.Time         `json:"created_at"`
}

func (s *Service) view(c Campaign, av audienceView, longest map[content.Channel]content.Values) View {
	v := View{
		ID: c.ID, Name: c.Name, Status: c.Status, Brief: c.Brief, Tone: c.Tone, Channels: c.Channels,
		Audience: av, Rule: c.Rule, UnsupportedCriteria: c.UnsupportedCriteria, AsOf: c.AsOf,
		LLM:      llmView{Mode: c.LLMMode, Model: c.LLMModel},
		Messages: map[content.Channel]*MessageView{}, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	for _, ch := range c.Channels {
		m, ok := c.Messages[ch]
		if !ok {
			v.Messages[ch] = nil // not drafted yet
			continue
		}
		mv := messageView(ch, m, longest[ch])
		v.Messages[ch] = &mv
	}
	return v
}

func messageView(ch content.Channel, m StoredMessage, longest content.Values) MessageView {
	mv := MessageView{State: m.State, Source: m.Source, Attempts: m.Attempts, Problems: m.Problems, UpdatedAt: m.UpdatedAt}
	if mv.Problems == nil {
		mv.Problems = []string{}
	}
	if hasContent(ch, m.Message) {
		msg := m.Message
		report := content.Validate(ch, msg, longest)
		mv.Message, mv.Report = &msg, &report
	}
	return mv
}
