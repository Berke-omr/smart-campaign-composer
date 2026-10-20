package segment

import (
	"context"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
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

func TestReach(t *testing.T) {
	a := Audience{Total: 177, SMSReachable: 99, EmailReachable: 133}
	both := a.Reach([]content.Channel{content.SMS, content.Email})
	if both.SMSExcluded != 78 || both.EmailExcluded != 44 || both.ChannelMessages != 232 {
		t.Errorf("both channels = %+v, want 78 / 44 excluded and 232 messages", both)
	}
	smsOnly := a.Reach([]content.Channel{content.SMS})
	if smsOnly.ChannelMessages != 99 || smsOnly.EmailExcluded != 44 {
		t.Errorf("SMS only = %+v, want 99 messages (exclusions are still reported)", smsOnly)
	}
}

// oracleLongest is LongestValues written in Go over the generated users.
// It returns the maximum length per variable and channel; values with that
// length are all acceptable answers (ties are ordered by the DB collation).
func oracleLongest(us []seed.User, g genRule) map[content.Channel]map[content.Variable]int {
	out := map[content.Channel]map[content.Variable]int{content.SMS: {}, content.Email: {}}
	for _, u := range us {
		if !oracleMatches(u, g) {
			continue
		}
		reach := map[content.Channel]bool{
			content.SMS:   u.SMSOptIn && u.Phone != nil,
			content.Email: u.EmailOptIn && u.Email != nil,
		}
		for ch, ok := range reach {
			if !ok {
				continue
			}
			for v, val := range map[content.Variable]string{content.FirstName: u.FirstName, content.LastName: u.LastName, content.City: u.City} {
				out[ch][v] = max(out[ch][v], utf8.RuneCountInString(val))
			}
		}
	}
	return out
}

func TestLongestValuesMatchOracle(t *testing.T) {
	pool, reg, generated := seededDB(t, 5000)
	counter := NewCounter(pool)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(9, 9))

	check := func(raw []byte, g genRule) {
		t.Helper()
		v, err := ParseAndValidate(raw, reg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := counter.LongestValues(ctx, v, asOf)
		if err != nil {
			t.Fatalf("LongestValues %s: %v", raw, err)
		}
		want := oracleLongest(generated, g)
		for _, ch := range content.Channels {
			for _, variable := range content.Variables {
				if n := utf8.RuneCountInString(got[ch][variable]); n != want[ch][variable] {
					t.Fatalf("rule %s, %s %s = %q (%d characters), oracle says %d",
						raw, ch, variable, got[ch][variable], n, want[ch][variable])
				}
			}
		}
	}

	r, _ := Parse([]byte(sampleBrief))
	check([]byte(sampleBrief), genRule{match: r.Match, conds: []genCondition{
		{field: "city", op: OpEq, strs: []string{"İstanbul"}},
		{field: "last_cart_add_at", op: OpWithinLastDays, days: 30},
		{field: "last_purchase_at", op: OpNotWithinLastDays, days: 30},
	}})
	for range 60 {
		raw, g := randomRule(rng, reg)
		check(raw, g)
	}

	v, _ := ParseAndValidate([]byte(sampleBrief), reg)
	got, _ := counter.LongestValues(ctx, v, asOf)
	t.Logf("sample brief longest values: SMS %v, email %v", got[content.SMS], got[content.Email])
}

func TestLongestValuesOfEmptyAudience(t *testing.T) {
	pool, reg, _ := seededDB(t, 200)
	v, err := ParseAndValidate([]byte(`{"match":"all","conditions":[{"field":"order_count","op":"gt","value":1000}]}`), reg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewCounter(pool).LongestValues(context.Background(), v, asOf)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range content.Channels {
		for _, variable := range content.Variables {
			if got[ch][variable] != "" {
				t.Errorf("%s %s = %q, want empty", ch, variable, got[ch][variable])
			}
		}
	}
}

func TestPostgresCountsCharactersLikeGo(t *testing.T) {
	// The worst-case check compares Postgres's char_length with Go's rune
	// count; they must agree on every name and city in the seed.
	pool, _, _ := seededDB(t, 5000)
	rows, err := pool.Query(context.Background(),
		`SELECT v, char_length(v) FROM (SELECT first_name FROM users UNION SELECT last_name FROM users UNION SELECT city FROM users) AS t(v)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n, multiByte := 0, 0
	for rows.Next() {
		var v string
		var pgLen int
		if err := rows.Scan(&v, &pgLen); err != nil {
			t.Fatal(err)
		}
		if goLen := utf8.RuneCountInString(v); goLen != pgLen {
			t.Errorf("%q: Postgres %d, Go %d", v, pgLen, goLen)
		}
		if len(v) != pgLen {
			multiByte++
		}
		n++
	}
	if multiByte == 0 {
		t.Fatalf("none of %d values has a multi-byte character; the test proves nothing", n)
	}
}

// TestRecipientsMatchOracle: for random rules, the people Recipients returns
// for each channel are exactly those the oracle finds reachable on it, with
// their own address and data. These are the people the approval freezes
// into deliveries, so this is R4 at the point where messages are created.
func TestRecipientsMatchOracle(t *testing.T) {
	pool, reg, generated := seededDB(t, 3000)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(11, 11))

	// Recipients runs inside the approval's transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	nonEmpty := 0
	for range 100 {
		raw, g := randomRule(rng, reg)
		v, err := ParseAndValidate(raw, reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range content.Channels {
			got, err := Recipients(ctx, tx, v, asOf, ch)
			if err != nil {
				t.Fatalf("Recipients %s: %v", raw, err)
			}
			var want []Recipient
			for i, u := range generated { // ids follow insertion order
				if !oracleMatches(u, g) {
					continue
				}
				var addr *string
				if ch == content.SMS && u.SMSOptIn {
					addr = u.Phone
				}
				if ch == content.Email && u.EmailOptIn {
					addr = u.Email
				}
				if addr == nil {
					continue
				}
				want = append(want, Recipient{UserID: int64(i + 1), Address: *addr,
					Values: content.Values{content.FirstName: u.FirstName, content.LastName: u.LastName, content.City: u.City}})
			}
			if len(got) != len(want) {
				t.Fatalf("rule %s, %s: %d recipients, oracle %d", raw, ch, len(got), len(want))
			}
			for i := range got {
				if got[i].UserID != want[i].UserID || got[i].Address != want[i].Address || !maps.Equal(got[i].Values, want[i].Values) {
					t.Fatalf("rule %s, %s recipient %d = %+v, oracle %+v", raw, ch, i, got[i], want[i])
				}
			}
			if len(want) > 0 {
				nonEmpty++
			}
		}
	}
	if nonEmpty < 100 {
		t.Errorf("only %d of 200 rule/channel pairs had recipients", nonEmpty)
	}
}
