package segment

import (
	"fmt"
	"maps"
	"slices"
)

// RuleSchema returns the JSON Schema of a Rule, for Claude's structured
// output (P3). It is generated from the registry, so the model's schema and
// the validator cannot drift apart.
//
// Each condition is an anyOf of one variant per (field, operator group): e.g.
// city + {eq, neq} + a city enum. Constrained decoding therefore already rules
// out incompatible field/operator/value combinations and unknown cities.
//
// Structured Outputs supports only a JSON Schema subset: no recursion, every
// object closed with additionalProperties:false, and no numeric or length
// bounds (minimum, maxLength, minItems, …). Ranges are therefore written into
// descriptions for the model and enforced by Validate.
func RuleSchema(reg *Registry) map[string]any {
	var variants []any
	for _, f := range reg.fields {
		for _, g := range opGroups(f) {
			variants = append(variants, closedObject(map[string]any{
				"field": map[string]any{"type": "string", "const": f.Name, "description": f.Description},
				"op":    map[string]any{"type": "string", "enum": opStrings(g.ops)},
				"value": valueSchema(f, g.shape),
			}))
		}
	}
	return closedObject(map[string]any{
		"match": map[string]any{
			"type":        "string",
			"enum":        []string{string(MatchAll), string(MatchAny)},
			"description": `"all": every condition must hold (AND). "any": at least one must hold (OR).`,
		},
		"conditions": map[string]any{
			"type":        "array",
			"description": fmt.Sprintf("At most %d conditions. An empty list targets all users.", MaxConditions),
			"items":       map[string]any{"anyOf": variants},
		},
	})
}

type opGroup struct {
	shape valueShape
	ops   []Op
}

// opGroups splits a field's operators by the value shape they take,
// preserving registry order.
func opGroups(f Field) []opGroup {
	var groups []opGroup
	index := map[valueShape]int{}
	for _, op := range f.Ops {
		s := op.shape()
		i, ok := index[s]
		if !ok {
			i = len(groups)
			index[s] = i
			groups = append(groups, opGroup{shape: s})
		}
		groups[i].ops = append(groups[i].ops, op)
	}
	return groups
}

func valueSchema(f Field, s valueShape) map[string]any {
	switch s {
	case shapeNone:
		return map[string]any{"type": "null"}
	case shapeDays:
		return map[string]any{"type": "integer",
			"description": fmt.Sprintf("Number of days, %d to %d.", MinDays, MaxDays)}
	case shapeList:
		return map[string]any{"type": "array", "items": enumSchema(f),
			"description": fmt.Sprintf("1 to %d values.", MaxListValues)}
	case shapePair:
		return map[string]any{"type": "array", "items": map[string]any{"type": numberType(f)},
			"description": fmt.Sprintf("Exactly two values [min, max], inclusive, each between %v and %v.", f.Min, f.Max)}
	default: // shapeScalar
		if f.Type == TypeEnum {
			return enumSchema(f)
		}
		return map[string]any{"type": numberType(f),
			"description": fmt.Sprintf("Between %v and %v.", f.Min, f.Max)}
	}
}

func enumSchema(f Field) map[string]any {
	return map[string]any{"type": "string", "enum": append([]string(nil), f.Enum...)}
}

func numberType(f Field) string {
	if f.Type == TypeInteger {
		return "integer"
	}
	return "number"
}

// closedObject builds an object schema where every property is required and
// no other property is allowed (Structured Outputs requires both). Keys are
// sorted so the schema serializes byte-identically every time, which keeps
// the prompt cache valid in P3.
func closedObject(props map[string]any) map[string]any {
	required := slices.Sorted(maps.Keys(props))
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func opStrings(ops []Op) []string {
	s := make([]string, len(ops))
	for i, o := range ops {
		s[i] = string(o)
	}
	return s
}
