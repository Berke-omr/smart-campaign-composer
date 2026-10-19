package segment

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func validate(t *testing.T, rule string) (Validated, Errors) {
	t.Helper()
	v, err := ParseAndValidate([]byte(rule), NewRegistry(testCities))
	if err == nil {
		return v, nil
	}
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("ParseAndValidate returned %T, want segment.Errors", err)
	}
	return v, errs
}

// one wraps a single condition in a rule.
func one(cond string) string {
	return `{"match":"all","conditions":[` + cond + `]}`
}

func TestValidateAcceptsEveryOperator(t *testing.T) {
	valid := []string{
		`{"field":"city","op":"eq","value":"İstanbul"}`,
		`{"field":"city","op":"neq","value":"Ankara"}`,
		`{"field":"city","op":"in","value":["İzmir","Bursa"]}`,
		`{"field":"city","op":"not_in","value":["Ankara"]}`,
		`{"field":"language","op":"eq","value":"en"}`,
		`{"field":"last_cart_add_at","op":"within_last_days","value":30}`,
		`{"field":"last_purchase_at","op":"not_within_last_days","value":30}`,
		`{"field":"last_purchase_at","op":"is_null"}`,
		`{"field":"last_purchase_at","op":"is_not_null","value":null}`,
		`{"field":"created_at","op":"within_last_days","value":730}`,
		`{"field":"order_count","op":"gte","value":3}`,
		`{"field":"order_count","op":"eq","value":0}`,
		`{"field":"order_count","op":"between","value":[1,5]}`,
		`{"field":"total_spent","op":"gt","value":1500.50}`,
		`{"field":"total_spent","op":"between","value":[100,100]}`,
		`{"field":"last_cart_add_at","op":"within_last_days","value":30.0}`, // whole number written as float
	}
	for _, cond := range valid {
		t.Run(cond, func(t *testing.T) {
			if _, errs := validate(t, one(cond)); errs != nil {
				t.Fatalf("rejected: %v", errs)
			}
		})
	}
}

func TestValidateEmptyRuleMeansAllUsers(t *testing.T) {
	v, errs := validate(t, `{"match":"all","conditions":[]}`)
	if errs != nil || v.Len() != 0 {
		t.Fatalf("empty rule: errs=%v len=%d", errs, v.Len())
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name     string
		rule     string
		wantCode ErrorCode
		wantMsg  string // substring of the message
	}{
		{"missing match", `{"conditions":[]}`, CodeInvalidMatch, `"all" or "any"`},
		{"bad match", `{"match":"some","conditions":[]}`, CodeInvalidMatch, `got "some"`},
		{"unknown field", one(`{"field":"gender","op":"eq","value":"f"}`), CodeUnknownField, "allowed fields: city, language"},
		{"pii field", one(`{"field":"email","op":"eq","value":"a@example.com"}`), CodeUnknownField, `"email"`},
		{"consent field", one(`{"field":"sms_opt_in","op":"eq","value":false}`), CodeUnknownField, `"sms_opt_in"`},
		{"operator not for type", one(`{"field":"city","op":"gt","value":"A"}`), CodeUnsupportedOperator, "allowed: eq, neq, in, not_in"},
		{"unknown operator", one(`{"field":"order_count","op":"like","value":"%"}`), CodeUnsupportedOperator, `"like"`},
		{"is_null on non-nullable", one(`{"field":"created_at","op":"is_null"}`), CodeUnsupportedOperator, "created_at"},
		{"city not in data", one(`{"field":"city","op":"eq","value":"Paris"}`), CodeInvalidValue, `allowed: "İstanbul"`},
		{"city wrong case", one(`{"field":"city","op":"eq","value":"istanbul"}`), CodeInvalidValue, `"istanbul" is not a known value`},
		{"enum given number", one(`{"field":"language","op":"eq","value":1}`), CodeInvalidValue, "must be a string"},
		{"enum missing value", one(`{"field":"language","op":"eq"}`), CodeInvalidValue, "got nothing"},
		{"in given scalar", one(`{"field":"city","op":"in","value":"Ankara"}`), CodeInvalidValue, "array of strings"},
		{"in empty list", one(`{"field":"city","op":"in","value":[]}`), CodeInvalidValue, "1 to 20 values"},
		{"in unknown member", one(`{"field":"city","op":"in","value":["Ankara","Paris"]}`), CodeInvalidValue, `"Paris"`},
		{"days zero", one(`{"field":"last_cart_add_at","op":"within_last_days","value":0}`), CodeOutOfRange, "between 1 and 730"},
		{"days too large", one(`{"field":"last_cart_add_at","op":"within_last_days","value":731}`), CodeOutOfRange, "got 731"},
		{"days fractional", one(`{"field":"last_cart_add_at","op":"within_last_days","value":7.5}`), CodeInvalidValue, "whole number"},
		{"days as string", one(`{"field":"last_cart_add_at","op":"within_last_days","value":"30"}`), CodeInvalidValue, "whole number"},
		{"null op given value", one(`{"field":"last_purchase_at","op":"is_null","value":5}`), CodeInvalidValue, "takes no value"},
		{"negative count", one(`{"field":"order_count","op":"gt","value":-1}`), CodeOutOfRange, "between 0 and 1000"},
		{"fractional count", one(`{"field":"order_count","op":"gt","value":2.5}`), CodeInvalidValue, "whole number"},
		{"between needs pair", one(`{"field":"order_count","op":"between","value":[1]}`), CodeInvalidValue, "[min, max]"},
		{"between reversed", one(`{"field":"total_spent","op":"between","value":[500,100]}`), CodeInvalidValue, "must not be greater"},
		{"between out of range", one(`{"field":"order_count","op":"between","value":[1,5000]}`), CodeOutOfRange, "max:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := validate(t, tt.rule)
			if len(errs) != 1 {
				t.Fatalf("want exactly one error, got %v", errs)
			}
			if errs[0].Code != tt.wantCode {
				t.Errorf("code = %s, want %s (%s)", errs[0].Code, tt.wantCode, errs[0].Message)
			}
			if !strings.Contains(errs[0].Message, tt.wantMsg) {
				t.Errorf("message %q does not contain %q", errs[0].Message, tt.wantMsg)
			}
		})
	}
}

// Injection attempts must die in validation: none of these strings is a
// known field, operator or enum value, so nothing reaches the compiler.
func TestValidateRejectsInjectionAttempts(t *testing.T) {
	attempts := []string{
		one(`{"field":"city; DROP TABLE users;--","op":"eq","value":"Ankara"}`),
		one(`{"field":"city\" OR 1=1 --","op":"eq","value":"Ankara"}`),
		one(`{"field":"city","op":"eq; DELETE FROM users","value":"Ankara"}`),
		one(`{"field":"city","op":"eq","value":"İstanbul' OR '1'='1"}`),
		one(`{"field":"city","op":"in","value":["Ankara","x') OR TRUE --"]}`),
		one(`{"field":"order_count","op":"gt","value":"0 OR 1=1"}`),
		one(`{"field":"last_cart_add_at","op":"within_last_days","value":"30 days' OR TRUE --"}`),
		`{"match":"all) OR (TRUE","conditions":[]}`,
	}
	for _, rule := range attempts {
		t.Run(rule, func(t *testing.T) {
			if _, errs := validate(t, rule); errs == nil {
				t.Fatal("injection attempt was accepted")
			}
		})
	}
}

func TestValidateReportsAllProblemsWithIndexes(t *testing.T) {
	rule := `{"match":"maybe","conditions":[
		{"field":"city","op":"eq","value":"Ankara"},
		{"field":"gender","op":"eq","value":"f"},
		{"field":"order_count","op":"gt","value":-5}
	]}`
	_, errs := validate(t, rule)
	got := make([]string, len(errs))
	for i, e := range errs {
		got[i] = fmt.Sprintf("%d:%s", e.Index, e.Code)
	}
	want := "-1:invalid_match 1:unknown_field 2:out_of_range"
	if strings.Join(got, " ") != want {
		t.Errorf("errors = %v, want %s", got, want)
	}
}

func TestValidateTooManyConditions(t *testing.T) {
	conds := make([]string, MaxConditions+1)
	for i := range conds {
		conds[i] = `{"field":"order_count","op":"gte","value":1}`
	}
	_, errs := validate(t, `{"match":"all","conditions":[`+strings.Join(conds, ",")+`]}`)
	if len(errs) != 1 || errs[0].Code != CodeTooManyConditions {
		t.Fatalf("errors = %v, want one too_many_conditions", errs)
	}
}

func TestValidateDeduplicatesListValues(t *testing.T) {
	v, errs := validate(t, one(`{"field":"city","op":"in","value":["Ankara","İzmir","Ankara"]}`))
	if errs != nil {
		t.Fatal(errs)
	}
	if got := v.conditions[0].strs; len(got) != 2 || got[0] != "Ankara" || got[1] != "İzmir" {
		t.Errorf("values = %v, want [Ankara İzmir]", got)
	}
}

func TestMalformedJSONIsReportedAsErrors(t *testing.T) {
	_, errs := validate(t, `{"match":"all","conditions":[`)
	if len(errs) != 1 || errs[0].Code != CodeInvalidJSON || errs[0].Index != -1 {
		t.Fatalf("errors = %v, want one invalid_json", errs)
	}
}
