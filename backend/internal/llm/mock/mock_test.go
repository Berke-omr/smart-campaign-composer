package mock

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

var testCities = []string{
	"İstanbul", "Ankara", "İzmir", "Bursa", "Antalya", "Adana", "Konya", "Gaziantep",
	"Kocaeli", "Eskişehir", "Mersin", "Kayseri", "Samsun", "Trabzon", "Diyarbakır",
}

// The longest values anywhere in the seed: content that fits these fits
// every audience.
var seedLongest = content.Values{
	content.FirstName: "Muhammed Emin",
	content.LastName:  "Montgomery-Fitzgerald",
	content.City:      "Diyarbakır",
}

func catalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return c
}

// outcome runs one scripted attempt through the same validators the
// generator uses: "valid", "invalid", "refusal" or "timeout".
func outcome(t *testing.T, task llm.Task, a Attempt, reg *segment.Registry) string {
	t.Helper()
	switch a.Fault {
	case "refusal":
		return "refusal"
	case "timeout":
		return "timeout"
	}
	if task == llm.TaskSegment {
		p, err := llm.ParseSegment(a.Text())
		if err != nil {
			return "invalid"
		}
		if _, err := segment.ParseAndValidate(p.Rule, reg); err != nil {
			return "invalid"
		}
		return "valid"
	}
	ch := content.Channel(task)
	m, err := llm.ParseDraft(ch, a.Text())
	if err != nil {
		return "invalid"
	}
	if !content.Validate(ch, m, seedLongest).Valid {
		return "invalid"
	}
	return "valid"
}

// simulate applies the attempt budget: 3 attempts, and a refusal is
// retried once (a second refusal is final).
func simulate(t *testing.T, s *Scenario, task llm.Task, reg *segment.Registry) string {
	t.Helper()
	list := s.Attempts(task)
	refusals := 0
	for attempt := 1; attempt <= 3; attempt++ {
		a := list[min(attempt, len(list))-1]
		switch outcome(t, task, a, reg) {
		case "valid":
			return fmt.Sprintf("valid@%d", attempt)
		case "refusal":
			refusals++
			if refusals == 2 {
				return "needs_manual_edit"
			}
		}
	}
	return "needs_manual_edit"
}

func TestCatalogExpectationsHoldAgainstTheValidators(t *testing.T) {
	reg := segment.NewRegistry(testCities)
	for _, s := range catalog(t).Scenarios() {
		t.Run(s.ID, func(t *testing.T) {
			tasks := []llm.Task{llm.TaskSegment}
			for _, ch := range s.Channels {
				tasks = append(tasks, llm.Task(ch))
			}
			if len(s.Expect) != len(tasks) {
				t.Fatalf("expect = %v, want one entry for each of %v", s.Expect, tasks)
			}
			for _, task := range tasks {
				if got, want := simulate(t, s, task, reg), s.Expect[string(task)]; got != want {
					t.Errorf("%s: scripted answers give %s, scenario expects %s", task, got, want)
				}
			}
			if s.Example {
				// Examples are what a reviewer clicks first: they must end
				// valid, and the segment must be valid on the first try.
				if s.Expect["segment"] != "valid@1" {
					t.Errorf("example segment = %s", s.Expect["segment"])
				}
				for _, ch := range s.Channels {
					if !strings.HasPrefix(s.Expect[string(ch)], "valid@") {
						t.Errorf("example %s = %s", ch, s.Expect[string(ch)])
					}
				}
			}
		})
	}
}

func TestCatalogCoversEveryFault(t *testing.T) {
	// PLAN.md P3: one scenario per fault.
	c := catalog(t)
	for _, id := range []string{"fault-broken-json", "fault-unknown-field", "fault-long-sms",
		"fault-unknown-variable", "fault-refusal", "fault-timeout", "fault-emoji"} {
		if _, ok := c.Scenario(id); !ok {
			t.Errorf("missing scenario %s", id)
		}
	}
	if s, _ := c.Scenario("cart-reminder-istanbul"); s == nil || !s.Example {
		t.Error("the case's sample brief must be an example")
	}
}

func TestExamples(t *testing.T) {
	ex := catalog(t).Examples()
	if len(ex) != 5 {
		t.Fatalf("%d examples, want 5", len(ex))
	}
	if ex[0].ID != "cart-reminder-istanbul" || !strings.HasPrefix(ex[0].Brief, "Son 30 günde") {
		t.Errorf("first example = %+v", ex[0])
	}
	for _, e := range ex {
		if strings.HasPrefix(e.Brief, "[test]") {
			t.Errorf("test scenario %s offered as an example", e.ID)
		}
	}
}

func TestMatchIgnoresWhitespaceAndCase(t *testing.T) {
	c := catalog(t)
	brief := "  SON 30 günde sepetine ürün ekleyip   alışveriş yapmayan İSTANBUL'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.\n"
	if s, ok := c.Match(brief); !ok || s.ID != "cart-reminder-istanbul" {
		t.Errorf("Match = %v, %v", s, ok)
	}
	if _, ok := c.Match("Son 30 günde sepetine ürün ekleyen herkese"); ok {
		t.Error("a different brief matched a scenario")
	}
}

func TestClientFollowsTheScriptPerAttempt(t *testing.T) {
	c := catalog(t)
	s, _ := c.Scenario("vip-thanks-izmir")
	m := New(c, Options{})
	ctx := context.Background()

	first, err := m.Complete(ctx, llm.Request{Task: llm.TaskSMS, Brief: s.Brief, Attempt: 1}, nil)
	if err != nil || first.Text != s.SMS[0].Text() {
		t.Fatalf("attempt 1 = %q, %v", first.Text, err)
	}
	second, _ := m.Complete(ctx, llm.Request{Task: llm.TaskSMS, Brief: s.Brief, Attempt: 2}, nil)
	third, _ := m.Complete(ctx, llm.Request{Task: llm.TaskSMS, Brief: s.Brief, Attempt: 3}, nil)
	if second.Text != s.SMS[1].Text() || third.Text != s.SMS[1].Text() {
		t.Errorf("attempts 2 and 3 should use the last entry: %q / %q", second.Text, third.Text)
	}
	if m.Calls() != 3 {
		t.Errorf("Calls = %d, want 3", m.Calls())
	}
}

func TestClientStreamsInFragments(t *testing.T) {
	c := catalog(t)
	s, _ := c.Scenario("cart-reminder-istanbul")
	var parts []string
	resp, err := New(c, Options{ChunkRunes: 5}).Complete(context.Background(),
		llm.Request{Task: llm.TaskEmail, Brief: s.Brief, Attempt: 1}, func(p string) { parts = append(parts, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 10 || strings.Join(parts, "") != resp.Text {
		t.Errorf("%d fragments; joined equals text: %v", len(parts), strings.Join(parts, "") == resp.Text)
	}
	for _, p := range parts {
		if len([]rune(p)) > 5 {
			t.Errorf("fragment %q longer than 5 characters", p)
		}
	}
}

func TestClientFaults(t *testing.T) {
	c := catalog(t)
	m := New(c, Options{})

	refusal, _ := c.Scenario("fault-refusal")
	_, err := m.Complete(context.Background(), llm.Request{Task: llm.TaskSMS, Brief: refusal.Brief, Attempt: 1}, nil)
	var r *llm.RefusalError
	if !errors.As(err, &r) || r.Category != "general_harms" {
		t.Errorf("refusal: err = %v", err)
	}

	timeout, _ := c.Scenario("fault-timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = m.Complete(ctx, llm.Request{Task: llm.TaskEmail, Brief: timeout.Brief, Attempt: 1}, func(string) {})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Errorf("timeout: err = %v after %v", err, time.Since(start))
	}
}

func TestStreamingStopsWhenTheCallerGivesUp(t *testing.T) {
	c := catalog(t)
	s, _ := c.Scenario("cart-reminder-istanbul")
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	_, err := New(c, Options{ChunkDelay: time.Millisecond, ChunkRunes: 1}).Complete(ctx,
		llm.Request{Task: llm.TaskEmail, Brief: s.Brief, Attempt: 1}, func(string) {
			n++
			if n == 3 {
				cancel()
			}
		})
	if !errors.Is(err, context.Canceled) || n != 3 {
		t.Errorf("err = %v after %d fragments, want Canceled after 3", err, n)
	}
}

func TestFallbackForUnknownBriefs(t *testing.T) {
	reg := segment.NewRegistry(testCities)
	m := New(catalog(t), Options{Cities: testCities})
	ctx := context.Background()
	tests := []struct {
		brief     string
		wantLines []string
	}{
		{"Ankara'daki kullanıcılardan son 14 günde sepete ürün ekleyip satın almayanlara %10 indirim gönder.",
			[]string{"City is Ankara", "Added to cart in the last 14 days", "No purchase in the last 14 days"}},
		{"İzmir ve Bursa'daki herkese yaz kampanyasını duyur.", []string{"City is one of İzmir, Bursa"}},
		{"Send a thank-you note to everyone.", nil},
	}
	for _, tt := range tests {
		resp, err := m.Complete(ctx, llm.Request{Task: llm.TaskSegment, Brief: tt.brief, Attempt: 1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := llm.ParseSegment(resp.Text)
		if err != nil {
			t.Fatalf("%q: %v", tt.brief, err)
		}
		v, err := segment.ParseAndValidate(p.Rule, reg)
		if err != nil {
			t.Fatalf("%q: fallback rule rejected: %v", tt.brief, err)
		}
		if got := segment.Describe(v).Lines; strings.Join(got, "|") != strings.Join(tt.wantLines, "|") {
			t.Errorf("%q: lines = %q, want %q", tt.brief, got, tt.wantLines)
		}

		for _, ch := range content.Channels {
			resp, err := m.Complete(ctx, llm.Request{Task: llm.Task(ch), Brief: tt.brief, Attempt: 1}, nil)
			if err != nil {
				t.Fatal(err)
			}
			msg, err := llm.ParseDraft(ch, resp.Text)
			if err != nil {
				t.Fatalf("%q %s: %v", tt.brief, ch, err)
			}
			if r := content.Validate(ch, msg, seedLongest); !r.Valid {
				t.Errorf("%q %s: fallback content invalid: %v", tt.brief, ch, r.Errors)
			}
		}
	}

	// The discount from the brief is carried into the copy.
	resp, _ := m.Complete(ctx, llm.Request{Task: llm.TaskSMS, Brief: tests[0].brief, Attempt: 1}, nil)
	if !strings.Contains(resp.Text, "%10 indirim") {
		t.Errorf("SMS = %s", resp.Text)
	}
}
