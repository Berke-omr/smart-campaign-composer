package segment

import (
	"math/rand/v2"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var asOf = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func compile(t *testing.T, rule string) Query {
	t.Helper()
	v, err := ParseAndValidate([]byte(rule), NewRegistry(testCities))
	if err != nil {
		t.Fatalf("invalid rule: %v", err)
	}
	return Compile(v, asOf)
}

func TestCompileEachOperator(t *testing.T) {
	days30 := asOf.AddDate(0, 0, -30)
	tests := []struct {
		cond      string
		wantWhere string
		wantArgs  []any
	}{
		{`{"field":"city","op":"eq","value":"İstanbul"}`, "(city = $1)", []any{"İstanbul"}},
		{`{"field":"city","op":"neq","value":"Ankara"}`, "(city <> $1)", []any{"Ankara"}},
		{`{"field":"city","op":"in","value":["İzmir","Bursa"]}`, "(city = ANY($1))", []any{[]string{"İzmir", "Bursa"}}},
		{`{"field":"city","op":"not_in","value":["Ankara"]}`, "(city <> ALL($1))", []any{[]string{"Ankara"}}},
		{`{"field":"order_count","op":"gt","value":2}`, "(order_count > $1)", []any{int64(2)}},
		{`{"field":"order_count","op":"lte","value":0}`, "(order_count <= $1)", []any{int64(0)}},
		{`{"field":"total_spent","op":"gte","value":99.9}`, "(total_spent >= $1)", []any{99.9}},
		{`{"field":"order_count","op":"between","value":[1,3]}`, "(order_count BETWEEN $1 AND $2)", []any{int64(1), int64(3)}},
		{`{"field":"last_cart_add_at","op":"within_last_days","value":30}`, "(last_cart_add_at >= $1)", []any{days30}},
		{
			`{"field":"last_purchase_at","op":"not_within_last_days","value":30}`,
			"((last_purchase_at IS NULL OR last_purchase_at < $1))", []any{days30},
		},
		{`{"field":"created_at","op":"not_within_last_days","value":30}`, "(created_at < $1)", []any{days30}},
		{`{"field":"last_purchase_at","op":"is_null"}`, "(last_purchase_at IS NULL)", nil},
		{`{"field":"last_purchase_at","op":"is_not_null"}`, "(last_purchase_at IS NOT NULL)", nil},
	}
	for _, tt := range tests {
		t.Run(tt.cond, func(t *testing.T) {
			q := compile(t, one(tt.cond))
			if q.Where != tt.wantWhere {
				t.Errorf("Where = %q, want %q", q.Where, tt.wantWhere)
			}
			if !reflect.DeepEqual(q.Args, tt.wantArgs) {
				t.Errorf("Args = %#v, want %#v", q.Args, tt.wantArgs)
			}
		})
	}
}

func TestCompileSampleBrief(t *testing.T) {
	// "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılar"
	q := compile(t, `{"match":"all","conditions":[
		{"field":"city","op":"eq","value":"İstanbul"},
		{"field":"last_cart_add_at","op":"within_last_days","value":30},
		{"field":"last_purchase_at","op":"not_within_last_days","value":30}]}`)

	want := "(city = $1 AND last_cart_add_at >= $2 AND (last_purchase_at IS NULL OR last_purchase_at < $3))"
	if q.Where != want {
		t.Errorf("Where = %q\nwant    %q", q.Where, want)
	}
	if len(q.Args) != 3 || q.Args[0] != "İstanbul" {
		t.Errorf("Args = %v", q.Args)
	}
}

func TestCompileMatchAnyAndEmpty(t *testing.T) {
	q := compile(t, `{"match":"any","conditions":[
		{"field":"city","op":"eq","value":"Ankara"},
		{"field":"order_count","op":"eq","value":0}]}`)
	if q.Where != "(city = $1 OR order_count = $2)" {
		t.Errorf("Where = %q", q.Where)
	}

	q = compile(t, `{"match":"any","conditions":[]}`)
	if q.Where != "TRUE" || len(q.Args) != 0 {
		t.Errorf("empty rule compiled to %q %v, want TRUE", q.Where, q.Args)
	}
}

func TestCompileUsesUTCCutoff(t *testing.T) {
	v, err := ParseAndValidate([]byte(one(`{"field":"last_cart_add_at","op":"within_last_days","value":1}`)), NewRegistry(testCities))
	if err != nil {
		t.Fatal(err)
	}
	istanbul := time.FixedZone("TRT", 3*60*60)
	q := Compile(v, asOf.In(istanbul))
	if got := q.Args[0].(time.Time); !got.Equal(asOf.AddDate(0, 0, -1)) || got.Location() != time.UTC {
		t.Errorf("cutoff = %v, want %v in UTC", got, asOf.AddDate(0, 0, -1))
	}
}

// sqlToken matches every token the compiler may legitimately emit. Anything
// else in the SQL text (a quote, a semicolon, a comment, an unknown word)
// would mean input leaked into the query.
var sqlToken = regexp.MustCompile(`^(\s+|\$\d+|[A-Za-z_]+|<>|>=|<=|[=<>(),])`)

var allowedWords = map[string]bool{
	"AND": true, "OR": true, "IS": true, "NOT": true, "NULL": true,
	"ANY": true, "ALL": true, "BETWEEN": true, "TRUE": true,
}

// assertSafeSQL checks the SQL shape: only known columns, keywords,
// operators and placeholders, and exactly one placeholder per argument.
func assertSafeSQL(t *testing.T, reg *Registry, q Query) {
	t.Helper()
	columns := map[string]bool{}
	for _, f := range reg.Fields() {
		columns[f.column] = true
	}
	rest := q.Where
	placeholders := map[int]bool{}
	for rest != "" {
		tok := sqlToken.FindString(rest)
		if tok == "" {
			t.Fatalf("unexpected text %q in SQL %q", rest, q.Where)
		}
		rest = rest[len(tok):]
		switch {
		case strings.TrimSpace(tok) == "":
		case tok[0] == '$':
			n, _ := strconv.Atoi(tok[1:])
			placeholders[n] = true
		case tok[0] >= 'A' && tok[0] <= 'Z' || tok[0] >= 'a' && tok[0] <= 'z' || tok[0] == '_':
			if !allowedWords[tok] && !columns[tok] {
				t.Fatalf("unexpected word %q in SQL %q", tok, q.Where)
			}
		}
	}
	if len(placeholders) != len(q.Args) {
		t.Fatalf("%d distinct placeholders but %d args in %q", len(placeholders), len(q.Args), q.Where)
	}
	for i := 1; i <= len(q.Args); i++ {
		if !placeholders[i] {
			t.Fatalf("placeholder $%d missing in %q", i, q.Where)
		}
	}
}

func TestCompiledSQLShapeForRandomRules(t *testing.T) {
	reg := NewRegistry(testCities)
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		raw, _ := randomRule(rng, reg)
		v, err := ParseAndValidate(raw, reg)
		if err != nil {
			t.Fatalf("generator produced an invalid rule %s: %v", raw, err)
		}
		assertSafeSQL(t, reg, Compile(v, asOf))
	}
}

// FuzzParseValidateCompile feeds arbitrary bytes through the whole pipeline.
// It must never panic, and anything that validates must compile to safe SQL.
// `go test` runs the seed corpus; `go test -fuzz=Fuzz` explores further.
func FuzzParseValidateCompile(f *testing.F) {
	seeds := []string{
		`{"match":"all","conditions":[{"field":"city","op":"eq","value":"İstanbul"},{"field":"last_cart_add_at","op":"within_last_days","value":30},{"field":"last_purchase_at","op":"not_within_last_days","value":30}]}`,
		`{"match":"any","conditions":[{"field":"order_count","op":"between","value":[1,3]},{"field":"total_spent","op":"gt","value":1e3}]}`,
		`{"match":"all","conditions":[{"field":"city; DROP TABLE users","op":"eq","value":"x"}]}`,
		`{"match":"all","conditions":[{"field":"city","op":"in","value":["Ankara","') OR TRUE --"]}]}`,
		`{"match":"all","conditions":[{"field":"last_purchase_at","op":"is_null","value":null}]}`,
		`{"match":"all","conditions":[]}`,
		`[]`, `null`, `{}`, `{"match":1}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	reg := NewRegistry(testCities)
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := ParseAndValidate(data, reg)
		if err != nil {
			return
		}
		assertSafeSQL(t, reg, Compile(v, asOf))
	})
}
