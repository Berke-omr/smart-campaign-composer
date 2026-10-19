// Package mock is the LLM used without an API key and in every test
// (DECISIONS.md D12). It answers from a scenario catalog: JSON files that
// script the model's answers for a brief, attempt by attempt, including
// faults (broken JSON, an unknown field, an over-long SMS, an unknown
// variable, a refusal, a timeout), so the repair loop runs the same way it
// would against Claude. A brief that matches no scenario gets a keyword-based
// answer, so a reviewer can still try their own brief.
package mock

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
)

//go:embed scenarios/*.json
var scenarioFiles embed.FS

// Scenario scripts the answers for one brief.
type Scenario struct {
	ID string `json:"id"`
	// Example scenarios are offered in the brief library (GET
	// /api/briefs/examples); the others exist for tests.
	Example  bool              `json:"example"`
	Title    string            `json:"title"`
	Brief    string            `json:"brief"`
	Channels []content.Channel `json:"channels"`
	Tone     llm.Tone          `json:"tone"`
	// One entry per attempt; when attempts run past the list, the last
	// entry repeats.
	Segment []Attempt `json:"segment"`
	SMS     []Attempt `json:"sms"`
	Email   []Attempt `json:"email"`
	// Expect is the outcome per task ("segment", "sms", "email"):
	// "valid@N" (valid on attempt N) or "needs_manual_edit". The catalog
	// test checks it against the validators, the generator tests against
	// the generator's events.
	Expect map[string]string `json:"expect"`
}

// Attempt is the scripted answer to one attempt: exactly one of a JSON
// answer, raw text, or a fault.
type Attempt struct {
	Answer   json.RawMessage `json:"answer,omitempty"`
	Raw      *string         `json:"raw,omitempty"`
	Fault    string          `json:"fault,omitempty"`    // "refusal" or "timeout"
	Category string          `json:"category,omitempty"` // refusal category
}

// Text is what the model "says" for this attempt.
func (a Attempt) Text() string {
	if a.Raw != nil {
		return *a.Raw
	}
	var b bytes.Buffer
	if err := json.Compact(&b, a.Answer); err != nil {
		return string(a.Answer)
	}
	return b.String()
}

// Attempts returns the scripted attempts for a task.
func (s *Scenario) Attempts(task llm.Task) []Attempt {
	switch task {
	case llm.TaskSegment:
		return s.Segment
	case llm.TaskSMS:
		return s.SMS
	case llm.TaskEmail:
		return s.Email
	}
	return nil
}

// Example is a ready-made brief for the UI's brief library.
type Example struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Brief    string            `json:"brief"`
	Channels []content.Channel `json:"channels"`
	Tone     llm.Tone          `json:"tone"`
}

// Catalog is the loaded set of scenarios, in file-name order.
type Catalog struct {
	scenarios []*Scenario
	byID      map[string]*Scenario
}

// LoadCatalog reads the embedded scenario files. A malformed file is an
// error, so a broken catalog stops the api at startup.
func LoadCatalog() (*Catalog, error) {
	names, err := fs.Glob(scenarioFiles, "scenarios/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	c := &Catalog{byID: map[string]*Scenario{}}
	briefs := map[string]string{}
	for _, name := range names {
		data, err := scenarioFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		var s Scenario
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := s.check(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if _, dup := c.byID[s.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate scenario id %q", name, s.ID)
		}
		key := normalize(s.Brief)
		if other, dup := briefs[key]; dup {
			return nil, fmt.Errorf("%s: same brief as scenario %q", name, other)
		}
		briefs[key] = s.ID
		c.byID[s.ID] = &s
		c.scenarios = append(c.scenarios, &s)
	}
	return c, nil
}

func (s *Scenario) check() error {
	if s.ID == "" || strings.TrimSpace(s.Brief) == "" {
		return fmt.Errorf("id and brief are required")
	}
	if s.Example && s.Title == "" {
		return fmt.Errorf("example %q needs a title", s.ID)
	}
	if !s.Tone.Valid() {
		return fmt.Errorf("unknown tone %q", s.Tone)
	}
	if len(s.Channels) == 0 {
		return fmt.Errorf("no channels")
	}
	if len(s.Segment) == 0 {
		return fmt.Errorf("no segment answers")
	}
	for _, ch := range s.Channels {
		if !ch.Valid() {
			return fmt.Errorf("unknown channel %q", ch)
		}
		if len(s.Attempts(llm.Task(ch))) == 0 {
			return fmt.Errorf("channel %s has no answers", ch)
		}
	}
	for task, list := range map[llm.Task][]Attempt{llm.TaskSegment: s.Segment, llm.TaskSMS: s.SMS, llm.TaskEmail: s.Email} {
		for i, a := range list {
			set := 0
			if len(a.Answer) > 0 {
				set++
			}
			if a.Raw != nil {
				set++
			}
			if a.Fault != "" {
				set++
				if a.Fault != "refusal" && a.Fault != "timeout" {
					return fmt.Errorf("%s attempt %d: unknown fault %q", task, i+1, a.Fault)
				}
			}
			if set != 1 {
				return fmt.Errorf("%s attempt %d: set exactly one of answer, raw, fault", task, i+1)
			}
		}
	}
	return nil
}

// Scenario looks a scenario up by id.
func (c *Catalog) Scenario(id string) (*Scenario, bool) {
	s, ok := c.byID[id]
	return s, ok
}

// Scenarios returns every scenario in file-name order.
func (c *Catalog) Scenarios() []*Scenario {
	return append([]*Scenario(nil), c.scenarios...)
}

// Match finds the scenario for a brief. Whitespace and letter case do not
// matter; any other change makes it a new brief.
func (c *Catalog) Match(brief string) (*Scenario, bool) {
	key := normalize(brief)
	for _, s := range c.scenarios {
		if normalize(s.Brief) == key {
			return s, true
		}
	}
	return nil, false
}

// Examples lists the example scenarios' briefs, in catalog order.
func (c *Catalog) Examples() []Example {
	out := []Example{}
	for _, s := range c.scenarios {
		if s.Example {
			out = append(out, Example{ID: s.ID, Title: s.Title, Brief: s.Brief, Channels: s.Channels, Tone: s.Tone})
		}
	}
	return out
}

func normalize(s string) string {
	return lowerTR(strings.Join(strings.Fields(s), " "))
}

// lowerTR lowercases with Turkish rules for dotted and dotless I
// ("İ" -> "i", "I" -> "ı"), which strings.ToLower does not apply.
func lowerTR(s string) string {
	return strings.ToLower(strings.NewReplacer("İ", "i", "I", "ı").Replace(s))
}
