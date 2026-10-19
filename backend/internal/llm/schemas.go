package llm

import (
	"encoding/json"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// Output schemas for Structured Outputs (DECISIONS.md D8). They stay inside
// the supported subset: closed objects, every property required, no length
// or numeric bounds. Limits are enforced by the validators instead.

// SegmentSchema is the answer schema for TaskSegment. The rule part comes
// from the segment registry, so it cannot drift from the validator.
func SegmentSchema(reg *segment.Registry) json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"campaign_name": map[string]any{
				"type":        "string",
				"description": "Short English title, two to five words.",
			},
			"rule": segment.RuleSchema(reg),
			"unsupported_criteria": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Audience criteria from the brief that no field can express. Empty when everything was expressed.",
			},
		},
		"required":             []string{"campaign_name", "rule", "unsupported_criteria"},
		"additionalProperties": false,
	}
	raw, err := json.Marshal(schema) // map keys are sorted: byte-identical every time
	if err != nil {
		panic(err) // a static structure of strings and maps cannot fail to marshal
	}
	return raw
}

// The content schemas are written out by hand so that the property order is
// fixed: the model generates properties in schema order, and the email's
// subject should arrive before its body.
var (
	smsSchema = json.RawMessage(`{"type":"object","properties":{` +
		`"text":{"type":"string","description":"The SMS text. At most 160 characters with variables filled in."}` +
		`},"required":["text"],"additionalProperties":false}`)

	emailSchema = json.RawMessage(`{"type":"object","properties":{` +
		`"subject":{"type":"string","description":"One line. At most 80 characters with variables filled in."},` +
		`"body":{"type":"string","description":"Plain text, paragraphs separated by a blank line."}` +
		`},"required":["subject","body"],"additionalProperties":false}`)
)

// ContentSchema is the answer schema for one channel's draft.
func ContentSchema(ch content.Channel) json.RawMessage {
	if ch == content.Email {
		return emailSchema
	}
	return smsSchema
}
