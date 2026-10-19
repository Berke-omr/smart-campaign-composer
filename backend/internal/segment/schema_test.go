package segment

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func compileSchema(t *testing.T, reg *Registry) *jsonschema.Schema {
	t.Helper()
	raw, err := json.Marshal(RuleSchema(reg))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("rule.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("rule.json")
	if err != nil {
		t.Fatalf("RuleSchema is not a valid JSON Schema: %v", err)
	}
	return sch
}

func conformsTo(t *testing.T, sch *jsonschema.Schema, rule string) bool {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(rule)))
	if err != nil {
		t.Fatalf("bad test JSON %s: %v", rule, err)
	}
	return sch.Validate(inst) == nil
}

// Keywords outside the Structured Outputs subset. If any appears, the API
// would reject the schema in P3.
var unsupportedKeywords = []string{
	"$ref", "$defs", "definitions", "minLength", "maxLength", "pattern",
	"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
	"minItems", "maxItems", "uniqueItems",
}

func TestRuleSchemaFitsStructuredOutputsSubset(t *testing.T) {
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch n := node.(type) {
		case map[string]any:
			for _, k := range unsupportedKeywords {
				if _, ok := n[k]; ok {
					t.Errorf("%s uses unsupported keyword %q", path, k)
				}
			}
			if n["type"] == "object" {
				if n["additionalProperties"] != false {
					t.Errorf("%s: object without additionalProperties:false", path)
				}
				props, _ := n["properties"].(map[string]any)
				required, _ := n["required"].([]string)
				if len(required) != len(props) {
					t.Errorf("%s: %d properties but %d required", path, len(props), len(required))
				}
			}
			if e, ok := n["enum"].([]string); ok && len(e) == 0 {
				t.Errorf("%s: empty enum", path)
			}
			for k, v := range n {
				walk(path+"."+k, v)
			}
		case []any:
			for i, v := range n {
				walk(path+"["+string(rune('0'+i%10))+"]", v)
			}
		}
	}
	walk("$", RuleSchema(NewRegistry(testCities)))
}

func TestRuleSchemaIsDeterministic(t *testing.T) {
	a, _ := json.Marshal(RuleSchema(NewRegistry(testCities)))
	b, _ := json.Marshal(RuleSchema(NewRegistry(testCities)))
	if !bytes.Equal(a, b) {
		t.Fatal("schema serialization differs between calls (would break prompt caching)")
	}
}

func TestRuleSchemaAcceptsEveryValidRule(t *testing.T) {
	reg := NewRegistry(testCities)
	sch := compileSchema(t, reg)
	rng := rand.New(rand.NewPCG(3, 4))
	for range 1000 {
		raw, _ := randomRule(rng, reg)
		// randomRule omits "value" for is_null ops; the schema requires an
		// explicit null, which is what the model will send.
		rule := bytes.ReplaceAll(raw, []byte(`"op":"is_null"}`), []byte(`"op":"is_null","value":null}`))
		rule = bytes.ReplaceAll(rule, []byte(`"op":"is_not_null"}`), []byte(`"op":"is_not_null","value":null}`))
		if !conformsTo(t, sch, string(rule)) {
			t.Fatalf("schema rejects valid rule %s", rule)
		}
	}
}

// With per-field variants, the schema already blocks these combinations at
// generation time (K1a). The validator still checks them for the mock LLM and
// API input.
func TestRuleSchemaRejectsIncompatibleCombinations(t *testing.T) {
	sch := compileSchema(t, NewRegistry(testCities))
	bad := map[string]string{
		"operator not for field": one(`{"field":"city","op":"gt","value":"Ankara"}`),
		"unknown city":           one(`{"field":"city","op":"eq","value":"Paris"}`),
		"days as string":         one(`{"field":"last_cart_add_at","op":"within_last_days","value":"30"}`),
		"null op with a value":   one(`{"field":"last_purchase_at","op":"is_null","value":5}`),
		"list op with scalar":    one(`{"field":"city","op":"in","value":"Ankara"}`),
		"unknown field":          one(`{"field":"gender","op":"eq","value":"f"}`),
		"extra key":              one(`{"field":"city","op":"eq","value":"Ankara","sql":"1=1"}`),
		"fractional count":       one(`{"field":"order_count","op":"gt","value":2.5}`),
		"bad match":              `{"match":"some","conditions":[]}`,
	}
	for name, rule := range bad {
		if conformsTo(t, sch, rule) {
			t.Errorf("%s: schema accepted %s", name, rule)
		}
	}
}

// Ranges cannot be expressed in the supported schema subset, so they are
// the validator's job. This test documents that split.
func TestRangesAreEnforcedByValidatorNotSchema(t *testing.T) {
	reg := NewRegistry(testCities)
	sch := compileSchema(t, reg)
	for _, rule := range []string{
		one(`{"field":"last_cart_add_at","op":"within_last_days","value":0}`),
		one(`{"field":"order_count","op":"between","value":[5,1]}`),
		one(`{"field":"city","op":"in","value":[]}`),
	} {
		if !conformsTo(t, sch, rule) {
			t.Errorf("expected schema to accept (range not expressible): %s", rule)
		}
		if _, err := ParseAndValidate([]byte(rule), reg); err == nil {
			t.Errorf("validator accepted out-of-range rule %s", rule)
		}
	}
}
