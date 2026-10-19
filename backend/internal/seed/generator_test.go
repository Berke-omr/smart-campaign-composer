package seed

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestGenerateIsDeterministic(t *testing.T) {
	a := Generate(300, 42, testNow)
	b := Generate(300, 42, testNow)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed produced different users")
	}
	if reflect.DeepEqual(a, Generate(300, 43, testNow)) {
		t.Fatal("different seeds produced identical users")
	}
}

var phoneRE = regexp.MustCompile(`^\+905[0-9]{9}$`)

// TestInvariants checks every rule the database also enforces (and a few it
// cannot), so a generator bug fails here with a readable message instead of
// as a COPY error.
func TestInvariants(t *testing.T) {
	users := Generate(5000, 42, testNow)
	cities := Cities()
	emails := make(map[string]bool, len(users))

	for i, u := range users {
		fail := func(format string, args ...any) {
			t.Helper()
			t.Errorf("user %d (%s %s): "+format, append([]any{i, u.FirstName, u.LastName}, args...)...)
		}
		if u.FirstName == "" || u.LastName == "" {
			fail("empty name")
		}
		if !slices.Contains(cities, u.City) {
			fail("unknown city %q", u.City)
		}
		if u.Language != "tr" && u.Language != "en" {
			fail("language %q", u.Language)
		}
		if u.SMSOptIn && u.Phone == nil {
			fail("SMS consent without phone")
		}
		if u.EmailOptIn && u.Email == nil {
			fail("email consent without email")
		}
		if u.Phone != nil && !phoneRE.MatchString(*u.Phone) {
			fail("phone %q", *u.Phone)
		}
		if u.Email != nil {
			e := *u.Email
			if !strings.HasSuffix(e, "@example.com") {
				fail("email %q not on reserved example.com", e)
			}
			if !isASCII(e) {
				fail("email %q is not ASCII", e)
			}
			if emails[e] {
				fail("duplicate email %q", e)
			}
			emails[e] = true
		}
		if (u.OrderCount == 0) != (u.LastPurchaseAt == nil) {
			fail("order_count=%d but last_purchase_at=%v", u.OrderCount, u.LastPurchaseAt)
		}
		if (u.OrderCount == 0) != (u.TotalSpentKurus == 0) {
			fail("order_count=%d but total_spent=%d", u.OrderCount, u.TotalSpentKurus)
		}
		if u.OrderCount > maxOrders {
			fail("order_count=%d", u.OrderCount)
		}
		for name, ts := range map[string]*time.Time{"last_cart_add_at": u.LastCartAddAt, "last_purchase_at": u.LastPurchaseAt} {
			if ts == nil {
				continue
			}
			if ts.After(testNow) {
				fail("%s %v is in the future", name, ts)
			}
			if ts.Before(u.CreatedAt) {
				fail("%s %v is before created_at %v", name, ts, u.CreatedAt)
			}
		}
	}
}

func TestDistribution(t *testing.T) {
	users := Generate(5000, 42, testNow)
	share := func(pred func(User) bool) float64 {
		n := 0
		for _, u := range users {
			if pred(u) {
				n++
			}
		}
		return float64(n) / float64(len(users))
	}
	tests := []struct {
		name     string
		got      float64
		min, max float64
	}{
		{"İstanbul", share(func(u User) bool { return u.City == "İstanbul" }), 0.27, 0.33},
		{"sms opt-in", share(func(u User) bool { return u.SMSOptIn }), 0.61, 0.68},
		{"email opt-in", share(func(u User) bool { return u.EmailOptIn }), 0.72, 0.78},
		{"never purchased", share(func(u User) bool { return u.OrderCount == 0 }), 0.32, 0.38},
		{"english", share(func(u User) bool { return u.Language == "en" }), 0.08, 0.12},
		{"cart in last 30 days", share(func(u User) bool {
			return u.LastCartAddAt != nil && u.LastCartAddAt.After(testNow.AddDate(0, 0, -30))
		}), 0.15, 0.25},
	}
	for _, tt := range tests {
		if tt.got < tt.min || tt.got > tt.max {
			t.Errorf("%s share = %.3f, want within [%.2f, %.2f]", tt.name, tt.got, tt.min, tt.max)
		}
	}
}

func TestNamesExerciseUnicodeAndLength(t *testing.T) {
	users := Generate(5000, 42, testNow)
	nonASCII, longest := 0, 0
	for _, u := range users {
		full := u.FirstName + " " + u.LastName
		if !isASCII(full) {
			nonASCII++
		}
		// Rune count, not len(): len("Ayşe") is 5 bytes but 4 characters.
		longest = max(longest, len([]rune(full)))
	}
	if nonASCII == 0 {
		t.Error("no generated name contains Turkish characters")
	}
	if longest < 25 {
		t.Errorf("longest full name has %d characters; want long names to test worst-case lengths", longest)
	}
}

func TestFold(t *testing.T) {
	tests := map[string]string{
		"Ayşe":                  "ayse",
		"İrem":                  "irem", // naive strings.ToLower would produce "i̇rem"
		"Çağrı":                 "cagri",
		"Ömer":                  "omer",
		"Ayşe Gül":              "aysegul",
		"Hacıosmanoğlu":         "haciosmanoglu",
		"Montgomery-Fitzgerald": "montgomeryfitzgerald",
	}
	for in, want := range tests {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
