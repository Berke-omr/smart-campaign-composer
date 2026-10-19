package segment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// ErrorCode classifies a validation problem. Codes are stable so the UI and
// the LLM repair loop can react to them.
type ErrorCode string

const (
	CodeInvalidJSON         ErrorCode = "invalid_json"
	CodeInvalidMatch        ErrorCode = "invalid_match"
	CodeTooManyConditions   ErrorCode = "too_many_conditions"
	CodeUnknownField        ErrorCode = "unknown_field"
	CodeUnsupportedOperator ErrorCode = "unsupported_operator"
	CodeInvalidValue        ErrorCode = "invalid_value"
	CodeOutOfRange          ErrorCode = "out_of_range"
)

// FieldError is one problem. Messages are written to be actionable by both a
// person and the LLM (they name the allowed alternatives).
type FieldError struct {
	Index   int       `json:"index"` // condition index; -1 for rule-level problems
	Field   string    `json:"field,omitempty"`
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Errors is every problem found in a rule; validation does not stop at the first.
type Errors []FieldError

func (e Errors) Error() string {
	msgs := make([]string, len(e))
	for i, fe := range e {
		if fe.Index >= 0 {
			msgs[i] = fmt.Sprintf("condition %d: %s", fe.Index, fe.Message)
		} else {
			msgs[i] = fe.Message
		}
	}
	return "invalid segment rule: " + strings.Join(msgs, "; ")
}

// Validated is a rule that passed validation. Its fields are unexported, so
// the only way to obtain one is Validate, and Compile accepts nothing else:
// an unchecked rule cannot reach SQL.
type Validated struct {
	match      Match
	conditions []validCondition
}

type validCondition struct {
	field *Field
	op    Op
	strs  []string  // eq/neq: one value; in/not_in: the list
	nums  []float64 // scalar comparisons: one value; between: [min, max]
	days  int       // relative-time operators
}

// Match reports how conditions are combined.
func (v Validated) Match() Match { return v.match }

// Len reports the number of conditions (0 means "all users").
func (v Validated) Len() int { return len(v.conditions) }

// ParseAndValidate is Parse followed by Validate. Every failure, including
// malformed JSON, is reported as Errors so callers (the API, the LLM repair
// loop) handle one error shape.
func ParseAndValidate(data []byte, reg *Registry) (Validated, error) {
	r, err := Parse(data)
	if err != nil {
		return Validated{}, Errors{{Index: -1, Code: CodeInvalidJSON, Message: err.Error()}}
	}
	return Validate(r, reg)
}

// Validate checks a rule against the registry and returns its typed form.
// An empty condition list is valid and means "all users".
func Validate(r Rule, reg *Registry) (Validated, error) {
	var errs Errors
	if r.Match != MatchAll && r.Match != MatchAny {
		errs = append(errs, FieldError{Index: -1, Code: CodeInvalidMatch,
			Message: fmt.Sprintf(`match must be "all" or "any", got %q`, r.Match)})
	}
	if len(r.Conditions) > MaxConditions {
		errs = append(errs, FieldError{Index: -1, Code: CodeTooManyConditions,
			Message: fmt.Sprintf("a rule may have at most %d conditions, got %d", MaxConditions, len(r.Conditions))})
	}

	v := Validated{match: r.Match, conditions: make([]validCondition, 0, len(r.Conditions))}
	for i, c := range r.Conditions {
		vc, fe := validateCondition(c, reg)
		if fe != nil {
			fe.Index = i
			errs = append(errs, *fe)
			continue
		}
		v.conditions = append(v.conditions, vc)
	}
	if len(errs) > 0 {
		return Validated{}, errs
	}
	return v, nil
}

func validateCondition(c Condition, reg *Registry) (validCondition, *FieldError) {
	field, ok := reg.Field(c.Field)
	if !ok {
		return validCondition{}, &FieldError{Field: c.Field, Code: CodeUnknownField,
			Message: fmt.Sprintf("unknown field %q; allowed fields: %s", c.Field, strings.Join(reg.FieldNames(), ", "))}
	}
	if !field.allows(c.Op) {
		return validCondition{}, &FieldError{Field: field.Name, Code: CodeUnsupportedOperator,
			Message: fmt.Sprintf("operator %q is not allowed for %s; allowed: %s", c.Op, field.Name, joinOps(field.Ops))}
	}

	vc := validCondition{field: field, op: c.Op}
	bad := func(code ErrorCode, format string, args ...any) (validCondition, *FieldError) {
		return validCondition{}, &FieldError{Field: field.Name, Code: code,
			Message: fmt.Sprintf("%s %s: ", field.Name, c.Op) + fmt.Sprintf(format, args...)}
	}

	switch c.Op.shape() {
	case shapeNone:
		if len(c.Value) > 0 && !isJSONNull(c.Value) {
			return bad(CodeInvalidValue, "takes no value, got %s", c.Value)
		}

	case shapeDays:
		var d float64
		if err := json.Unmarshal(c.Value, &d); err != nil || len(c.Value) == 0 {
			return bad(CodeInvalidValue, "value must be a whole number of days, got %s", printable(c.Value))
		}
		if d != math.Trunc(d) {
			return bad(CodeInvalidValue, "value must be a whole number of days, got %v", d)
		}
		if d < MinDays || d > MaxDays {
			return bad(CodeOutOfRange, "days must be between %d and %d, got %v", MinDays, MaxDays, d)
		}
		vc.days = int(d)

	case shapeScalar:
		if field.Type == TypeEnum {
			var s string
			if err := json.Unmarshal(c.Value, &s); err != nil || len(c.Value) == 0 {
				return bad(CodeInvalidValue, "value must be a string, got %s", printable(c.Value))
			}
			if !slices.Contains(field.Enum, s) {
				return bad(CodeInvalidValue, "%q is not a known value; allowed: %s", s, quoteAll(field.Enum))
			}
			vc.strs = []string{s}
		} else {
			n, code, problem := numberValue(field, c.Value)
			if code != "" {
				return bad(code, "%s", problem)
			}
			vc.nums = []float64{n}
		}

	case shapeList:
		var list []string
		if err := json.Unmarshal(c.Value, &list); err != nil || len(c.Value) == 0 || isJSONNull(c.Value) {
			return bad(CodeInvalidValue, "value must be an array of strings, got %s", printable(c.Value))
		}
		if len(list) == 0 || len(list) > MaxListValues {
			return bad(CodeInvalidValue, "list must have 1 to %d values, got %d", MaxListValues, len(list))
		}
		seen := make(map[string]bool, len(list))
		for _, s := range list {
			if !slices.Contains(field.Enum, s) {
				return bad(CodeInvalidValue, "%q is not a known value; allowed: %s", s, quoteAll(field.Enum))
			}
			if !seen[s] {
				seen[s] = true
				vc.strs = append(vc.strs, s)
			}
		}

	case shapePair:
		var raw []json.RawMessage
		if err := json.Unmarshal(c.Value, &raw); err != nil || len(raw) != 2 {
			return bad(CodeInvalidValue, "value must be [min, max], got %s", printable(c.Value))
		}
		lo, code, problem := numberValue(field, raw[0])
		if code != "" {
			return bad(code, "min: %s", problem)
		}
		hi, code, problem := numberValue(field, raw[1])
		if code != "" {
			return bad(code, "max: %s", problem)
		}
		if lo > hi {
			return bad(CodeInvalidValue, "min (%v) must not be greater than max (%v)", lo, hi)
		}
		vc.nums = []float64{lo, hi}
	}
	return vc, nil
}

// numberValue decodes and range-checks a numeric value. On failure it
// returns a non-empty code and a description the caller prefixes.
func numberValue(f *Field, raw json.RawMessage) (float64, ErrorCode, string) {
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || len(raw) == 0 || isJSONNull(raw) {
		return 0, CodeInvalidValue, fmt.Sprintf("value must be a number, got %s", printable(raw))
	}
	if f.Type == TypeInteger && n != math.Trunc(n) {
		return 0, CodeInvalidValue, fmt.Sprintf("value must be a whole number, got %v", n)
	}
	if n < f.Min || n > f.Max {
		return 0, CodeOutOfRange, fmt.Sprintf("value must be between %v and %v, got %v", f.Min, f.Max, n)
	}
	return n, "", ""
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func printable(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "nothing"
	}
	const max = 80
	if len(raw) > max {
		return string(raw[:max]) + "…"
	}
	return string(raw)
}

func joinOps(ops []Op) string {
	s := make([]string, len(ops))
	for i, o := range ops {
		s[i] = string(o)
	}
	return strings.Join(s, ", ")
}

func quoteAll(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = fmt.Sprintf("%q", x)
	}
	return strings.Join(q, ", ")
}
