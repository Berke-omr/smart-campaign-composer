package segment

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

const sampleBrief = `{"match":"all","conditions":[
	{"field":"city","op":"eq","value":"İstanbul"},
	{"field":"last_cart_add_at","op":"within_last_days","value":30},
	{"field":"last_purchase_at","op":"not_within_last_days","value":30}]}`

// seededDB loads n deterministic users generated relative to asOf and builds
// the registry from the cities actually in the database (as the api does).
func seededDB(t *testing.T, n int) (*pgxpool.Pool, *Registry, []seed.User) {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	generated := seed.Generate(n, 42, asOf)
	if _, err := seed.Load(ctx, pool, generated, false); err != nil {
		t.Fatal(err)
	}
	cities, err := users.NewRepository(pool).DistinctCities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return pool, NewRegistry(cities), generated
}

// TestAudienceMatchesOracle is a differential test: the same rules are
// evaluated by the SQL compiler and by an independent Go implementation of
// the DSL semantics (oracleMatches). Any disagreement — a wrong NULL rule, a
// flipped comparison, a missing consent filter — shows up as a count mismatch.
func TestAudienceMatchesOracle(t *testing.T) {
	pool, reg, generated := seededDB(t, 3000)
	counter := NewCounter(pool)
	rng := rand.New(rand.NewPCG(7, 7))

	nonTrivial := 0
	for range 300 {
		raw, g := randomRule(rng, reg)
		v, err := ParseAndValidate(raw, reg)
		if err != nil {
			t.Fatalf("generator produced an invalid rule %s: %v", raw, err)
		}
		got, err := counter.Count(context.Background(), v, asOf)
		if err != nil {
			t.Fatalf("count %s: %v", raw, err)
		}
		want := oracleAudience(generated, g)
		if got != want {
			t.Fatalf("rule %s\n  SQL    = %+v\n  oracle = %+v", raw, got, want)
		}
		if want.Total > 0 && want.Total < int64(len(generated)) {
			nonTrivial++
		}
	}
	// Guard against a vacuous test where every rule matches nobody or everybody.
	if nonTrivial < 150 {
		t.Errorf("only %d of 300 rules produced a non-trivial audience", nonTrivial)
	}
}

// TestSampleBriefThreeWays counts the case's sample brief via the compiler,
// via hand-written SQL and via the oracle.
func TestSampleBriefThreeWays(t *testing.T) {
	pool, reg, generated := seededDB(t, 5000)
	ctx := context.Background()

	v, err := ParseAndValidate([]byte(sampleBrief), reg)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := NewCounter(pool).Count(ctx, v, asOf)
	if err != nil {
		t.Fatal(err)
	}

	var hand Audience
	err = pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE sms_opt_in), count(*) FILTER (WHERE email_opt_in)
		FROM users
		WHERE city = 'İstanbul'
		  AND last_cart_add_at >= $1::timestamptz - interval '30 days'
		  AND (last_purchase_at IS NULL OR last_purchase_at < $1::timestamptz - interval '30 days')`,
		asOf).Scan(&hand.Total, &hand.SMSReachable, &hand.EmailReachable)
	if err != nil {
		t.Fatal(err)
	}

	r, _ := Parse([]byte(sampleBrief))
	oracle := oracleAudience(generated, genRule{match: r.Match, conds: []genCondition{
		{field: "city", op: OpEq, strs: []string{"İstanbul"}},
		{field: "last_cart_add_at", op: OpWithinLastDays, days: 30},
		{field: "last_purchase_at", op: OpNotWithinLastDays, days: 30},
	}})

	if compiled != hand || compiled != oracle {
		t.Fatalf("compiled=%+v hand-written=%+v oracle=%+v", compiled, hand, oracle)
	}
	if compiled.Total == 0 {
		t.Fatal("sample brief matched nobody; the seed no longer exercises it")
	}
	t.Logf("sample brief audience (5000 users, seed 42): %+v", compiled)
}

// TestNullSemantics pins the behaviour reviewers ask about, on hand-made rows.
func TestNullSemantics(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (first_name, last_name, city, phone, sms_opt_in, order_count, total_spent, last_purchase_at, created_at) VALUES
		('Never',  'Bought', 'Ankara', '+905000000001', true,  0, 0,   NULL,                          $1::timestamptz - interval '400 days'),
		('Recent', 'Buyer',  'Ankara', NULL,            false, 1, 100, $1::timestamptz - interval '10 days', $1::timestamptz - interval '400 days'),
		('Old',    'Buyer',  'Ankara', '+905000000003', false, 2, 300, $1::timestamptz - interval '60 days', $1::timestamptz - interval '400 days')`,
		asOf)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry([]string{"Ankara"})
	counter := NewCounter(pool)

	tests := []struct {
		cond     string
		want     int64
		whoMatch string
	}{
		{`{"field":"last_purchase_at","op":"within_last_days","value":30}`, 1, "Recent only: never-purchased is not 'recent'"},
		{`{"field":"last_purchase_at","op":"not_within_last_days","value":30}`, 2, "Never + Old: never-purchased counts as 'not recent'"},
		{`{"field":"last_purchase_at","op":"is_null"}`, 1, "Never"},
		{`{"field":"last_purchase_at","op":"is_not_null"}`, 2, "Recent + Old"},
		{`{"field":"order_count","op":"eq","value":0}`, 1, "Never"},
	}
	for _, tt := range tests {
		t.Run(tt.whoMatch, func(t *testing.T) {
			v, err := ParseAndValidate([]byte(one(tt.cond)), reg)
			if err != nil {
				t.Fatal(err)
			}
			got, err := counter.Count(ctx, v, asOf)
			if err != nil {
				t.Fatal(err)
			}
			if got.Total != tt.want {
				t.Errorf("total = %d, want %d (%s)", got.Total, tt.want, tt.whoMatch)
			}
		})
	}

	// Consent is applied by the backend: only "Never" has SMS consent.
	all, _ := ParseAndValidate([]byte(`{"match":"all","conditions":[]}`), reg)
	got, err := counter.Count(ctx, all, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Audience{Total: 3, SMSReachable: 1, EmailReachable: 0}) {
		t.Errorf("all users = %+v, want total 3, 1 SMS-reachable, 0 email-reachable", got)
	}
}

// ---- oracle: the DSL semantics written independently in plain Go ----

func oracleAudience(us []seed.User, g genRule) Audience {
	var a Audience
	for _, u := range us {
		if !oracleMatches(u, g) {
			continue
		}
		a.Total++
		if u.SMSOptIn && u.Phone != nil {
			a.SMSReachable++
		}
		if u.EmailOptIn && u.Email != nil {
			a.EmailReachable++
		}
	}
	return a
}

func oracleMatches(u seed.User, g genRule) bool {
	if len(g.conds) == 0 {
		return true
	}
	for _, c := range g.conds {
		m := oracleCondition(u, c)
		if g.match == MatchAll && !m {
			return false
		}
		if g.match == MatchAny && m {
			return true
		}
	}
	return g.match == MatchAll
}

func oracleCondition(u seed.User, c genCondition) bool {
	switch c.field {
	case "city":
		return oracleEnum(u.City, c)
	case "language":
		return oracleEnum(u.Language, c)
	case "last_cart_add_at":
		return oracleTime(u.LastCartAddAt, c)
	case "last_purchase_at":
		return oracleTime(u.LastPurchaseAt, c)
	case "created_at":
		created := u.CreatedAt
		return oracleTime(&created, c)
	case "order_count":
		return oracleNumber(int64(u.OrderCount), 1, c)
	case "total_spent":
		// Compare in kuruş so the oracle never touches floating point.
		return oracleNumber(u.TotalSpentKurus, 100, c)
	}
	panic("oracle: unknown field " + c.field)
}

func oracleEnum(v string, c genCondition) bool {
	switch c.op {
	case OpEq:
		return v == c.strs[0]
	case OpNeq:
		return v != c.strs[0]
	case OpIn:
		return slices.Contains(c.strs, v)
	case OpNotIn:
		return !slices.Contains(c.strs, v)
	}
	panic("oracle: enum op " + c.op)
}

func oracleTime(t *time.Time, c genCondition) bool {
	limit := asOf.AddDate(0, 0, -c.days)
	switch c.op {
	case OpWithinLastDays:
		return t != nil && !t.Before(limit)
	case OpNotWithinLastDays:
		return t == nil || t.Before(limit)
	case OpIsNull:
		return t == nil
	case OpIsNotNull:
		return t != nil
	}
	panic("oracle: time op " + c.op)
}

// oracleNumber compares v (in minor units) with rule values scaled by unit.
func oracleNumber(v, unit int64, c genCondition) bool {
	x := func(i int) int64 { return int64(c.nums[i]) * unit }
	switch c.op {
	case OpEq:
		return v == x(0)
	case OpNeq:
		return v != x(0)
	case OpGt:
		return v > x(0)
	case OpGte:
		return v >= x(0)
	case OpLt:
		return v < x(0)
	case OpLte:
		return v <= x(0)
	case OpBetween:
		return v >= x(0) && v <= x(1)
	}
	panic("oracle: number op " + c.op)
}
