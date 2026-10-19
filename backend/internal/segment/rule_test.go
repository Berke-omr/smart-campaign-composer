package segment

import (
	"slices"
	"strings"
	"testing"
)

var testCities = []string{"İstanbul", "Ankara", "İzmir", "Bursa"}

func TestRegistry(t *testing.T) {
	reg := NewRegistry(testCities)

	want := []string{"city", "language", "last_cart_add_at", "last_purchase_at", "created_at", "order_count", "total_spent"}
	if got := reg.FieldNames(); !slices.Equal(got, want) {
		t.Errorf("FieldNames() = %v, want %v", got, want)
	}

	city, ok := reg.Field("city")
	if !ok || !slices.Equal(city.Enum, testCities) {
		t.Fatalf("city field = %+v", city)
	}
	// The registry must not alias the caller's slice.
	cities := slices.Clone(testCities)
	reg2 := NewRegistry(cities)
	cities[0] = "Mutated"
	if f, _ := reg2.Field("city"); f.Enum[0] != "İstanbul" {
		t.Error("registry shares the cities slice with the caller")
	}

	for _, forbidden := range []string{"email", "phone", "first_name", "sms_opt_in", "email_opt_in"} {
		if _, ok := reg.Field(forbidden); ok {
			t.Errorf("field %q must not be targetable", forbidden)
		}
	}
}

func TestRegistryWithoutCitiesOmitsCity(t *testing.T) {
	if _, ok := NewRegistry(nil).Field("city"); ok {
		t.Error("city field present although no cities are known")
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string // empty = must succeed
	}{
		{name: "valid", in: `{"match":"all","conditions":[{"field":"city","op":"eq","value":"Ankara"}]}`},
		{name: "value may be omitted", in: `{"match":"all","conditions":[{"field":"last_purchase_at","op":"is_null"}]}`},
		{name: "no conditions", in: `{"match":"all","conditions":[]}`},
		{name: "unknown top-level key", in: `{"match":"all","conditions":[],"sql":"DROP TABLE users"}`, wantErr: `unknown field "sql"`},
		{name: "unknown condition key", in: `{"match":"all","conditions":[{"field":"city","op":"eq","value":"Ankara","raw":"1=1"}]}`, wantErr: `unknown field "raw"`},
		{name: "wrong type", in: `{"match":"all","conditions":{"field":"city"}}`, wantErr: "cannot unmarshal"},
		{name: "trailing data", in: `{"match":"all","conditions":[]} {"match":"any"}`, wantErr: "unexpected data"},
		{name: "not json", in: `match=all`, wantErr: "malformed rule JSON"},
		{name: "empty", in: ``, wantErr: "malformed rule JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("expected error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseKeepsRawValue(t *testing.T) {
	r, err := Parse([]byte(`{"match":"any","conditions":[{"field":"city","op":"in","value":["Ankara","İzmir"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Match != MatchAny || len(r.Conditions) != 1 {
		t.Fatalf("rule = %+v", r)
	}
	if got := string(r.Conditions[0].Value); got != `["Ankara","İzmir"]` {
		t.Errorf("raw value = %s", got)
	}
}
