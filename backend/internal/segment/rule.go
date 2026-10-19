package segment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Match combines conditions: "all" is AND, "any" is OR.
type Match string

const (
	MatchAll Match = "all"
	MatchAny Match = "any"
)

// Rule is the untrusted, as-received form of a segment rule (from the LLM or
// the API). It must go through Validate before it can be compiled.
type Rule struct {
	Match      Match       `json:"match"`
	Conditions []Condition `json:"conditions"`
}

// Condition is one comparison. Value is kept raw because its expected type
// depends on the field and operator; Validate decodes it.
type Condition struct {
	Field string          `json:"field"`
	Op    Op              `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// Parse decodes a rule strictly: unknown keys (e.g. an injected "sql"),
// wrong types and trailing data are errors.
func Parse(data []byte) (Rule, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var r Rule
	if err := dec.Decode(&r); err != nil {
		return Rule{}, fmt.Errorf("malformed rule JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Rule{}, errors.New("malformed rule JSON: unexpected data after the rule object")
	}
	return r, nil
}
