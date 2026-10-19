package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
)

// Parsing is validation layer 1, the shape of the answer (DECISIONS.md D10).
// It runs even though Structured Outputs already guarantees the shape,
// because the mock, recorded fixtures and future model or SDK changes must
// pass the same gate. Business rules (layer 2) live in the segment and
// content packages.

// Problems lists everything wrong with an answer, worded so that both a
// person and the model can act on it (the repair prompt quotes them).
type Problems []string

func (p Problems) Error() string { return strings.Join(p, "; ") }

// Limits on the segment answer's own fields.
const (
	MaxCampaignNameRunes   = 60
	MaxUnsupportedCriteria = 10
)

// SegmentProposal is a parsed TaskSegment answer. Rule is still untrusted:
// it must go through segment.ParseAndValidate before it can be counted.
type SegmentProposal struct {
	CampaignName        string
	Rule                json.RawMessage
	UnsupportedCriteria []string
}

type segmentWire struct {
	CampaignName        *string         `json:"campaign_name"`
	Rule                json.RawMessage `json:"rule"`
	UnsupportedCriteria *[]string       `json:"unsupported_criteria"`
}

// ParseSegment decodes a TaskSegment answer strictly. Errors are Problems.
func ParseSegment(text string) (SegmentProposal, error) {
	var w segmentWire
	if err := decodeStrict(text, &w); err != nil {
		return SegmentProposal{}, Problems{err.Error()}
	}
	var probs Problems
	if w.CampaignName == nil {
		probs = append(probs, "campaign_name is missing")
	}
	if len(w.Rule) == 0 || string(w.Rule) == "null" {
		probs = append(probs, "rule is missing")
	}
	if w.UnsupportedCriteria == nil {
		probs = append(probs, "unsupported_criteria is missing")
	}
	if len(probs) > 0 {
		return SegmentProposal{}, probs
	}

	p := SegmentProposal{CampaignName: strings.TrimSpace(*w.CampaignName), Rule: w.Rule}
	switch n := utf8.RuneCountInString(p.CampaignName); {
	case n == 0:
		probs = append(probs, "campaign_name is empty")
	case n > MaxCampaignNameRunes:
		probs = append(probs, fmt.Sprintf("campaign_name has %d characters; the limit is %d", n, MaxCampaignNameRunes))
	}
	if strings.ContainsAny(p.CampaignName, "\r\n") {
		probs = append(probs, "campaign_name must be a single line")
	}
	for _, c := range *w.UnsupportedCriteria {
		if c = strings.TrimSpace(c); c != "" {
			p.UnsupportedCriteria = append(p.UnsupportedCriteria, c)
		}
	}
	if len(p.UnsupportedCriteria) > MaxUnsupportedCriteria {
		probs = append(probs, fmt.Sprintf("unsupported_criteria has %d entries; the limit is %d", len(p.UnsupportedCriteria), MaxUnsupportedCriteria))
	}
	if p.UnsupportedCriteria == nil {
		p.UnsupportedCriteria = []string{}
	}
	if len(probs) > 0 {
		return SegmentProposal{}, probs
	}
	return p, nil
}

type smsWire struct {
	Text *string `json:"text"`
}

type emailWire struct {
	Subject *string `json:"subject"`
	Body    *string `json:"body"`
}

// ParseDraft decodes one channel's answer strictly. Whether the text is
// acceptable (length, variables) is content.Validate's job. Errors are
// Problems.
func ParseDraft(ch content.Channel, text string) (content.Message, error) {
	if ch == content.Email {
		var w emailWire
		if err := decodeStrict(text, &w); err != nil {
			return content.Message{}, Problems{err.Error()}
		}
		var probs Problems
		if w.Subject == nil {
			probs = append(probs, "subject is missing")
		}
		if w.Body == nil {
			probs = append(probs, "body is missing")
		}
		if len(probs) > 0 {
			return content.Message{}, probs
		}
		return content.Message{Subject: *w.Subject, Body: *w.Body}, nil
	}

	var w smsWire
	if err := decodeStrict(text, &w); err != nil {
		return content.Message{}, Problems{err.Error()}
	}
	if w.Text == nil {
		return content.Message{}, Problems{"text is missing"}
	}
	return content.Message{Text: *w.Text}, nil
}

// decodeStrict decodes one JSON object: unknown keys, wrong types and
// trailing data are errors.
func decodeStrict(text string, v any) error {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("the answer is not the expected JSON object: %v", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("the answer has extra data after the JSON object")
	}
	return nil
}
