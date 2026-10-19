package segment

import (
	"slices"
	"testing"
)

func TestDescribe(t *testing.T) {
	reg := NewRegistry(testCities)
	tests := []struct {
		cond string
		want string
	}{
		{`{"field":"city","op":"eq","value":"İstanbul"}`, "City is İstanbul"},
		{`{"field":"city","op":"not_in","value":["Ankara","Bursa"]}`, "City is none of Ankara, Bursa"},
		{`{"field":"language","op":"eq","value":"en"}`, "Language is English"},
		{`{"field":"order_count","op":"between","value":[1,3]}`, "Order count is between 1 and 3"},
		{`{"field":"total_spent","op":"gte","value":1500.5}`, "Total spent (TRY) is at least 1500.5"},
		{`{"field":"last_cart_add_at","op":"within_last_days","value":1}`, "Last added to cart: within the last 1 day"},
		{`{"field":"last_purchase_at","op":"not_within_last_days","value":30}`, "Last purchase: not within the last 30 days (or never)"},
		{`{"field":"created_at","op":"not_within_last_days","value":30}`, "Signed up: not within the last 30 days"},
		{`{"field":"last_purchase_at","op":"is_null"}`, "Last purchase: never"},
	}
	for _, tt := range tests {
		v, err := ParseAndValidate([]byte(one(tt.cond)), reg)
		if err != nil {
			t.Fatal(err)
		}
		if got := Describe(v).Lines; len(got) != 1 || got[0] != tt.want {
			t.Errorf("Describe(%s) = %q, want %q", tt.cond, got, tt.want)
		}
	}
}

func TestDescribeSummary(t *testing.T) {
	reg := NewRegistry(testCities)

	all, _ := ParseAndValidate([]byte(`{"match":"all","conditions":[]}`), reg)
	if d := Describe(all); d.Summary != "All users" || len(d.Lines) != 0 {
		t.Errorf("empty rule = %+v", d)
	}

	v, err := ParseAndValidate([]byte(sampleBrief), reg)
	if err != nil {
		t.Fatal(err)
	}
	want := Description{
		Summary: "Users matching all of the following",
		Lines: []string{
			"City is İstanbul",
			"Last added to cart: within the last 30 days",
			"Last purchase: not within the last 30 days (or never)",
		},
	}
	if d := Describe(v); d.Summary != want.Summary || !slices.Equal(d.Lines, want.Lines) {
		t.Errorf("Describe(sample brief) = %+v\nwant %+v", d, want)
	}
}
