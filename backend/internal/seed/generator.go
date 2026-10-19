// Package seed generates and loads the fake user base.
//
// Generation is pure and deterministic: the same (n, seed, now) always yields
// the same users, which makes counts reproducible in tests and demos.
package seed

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Distribution knobs. Expected overall shares (for n large):
//
//	İstanbul              ≈ 30%
//	has phone / email     = 92% / 95%
//	SMS / email opt-in    ≈ 64% / 75% (consent only exists with contact data)
//	never purchased       = 35%
//	cart activity ≤ 90d   = 55%
//	language en           = 10%
const (
	pEnglish        = 0.10
	pHasEmail       = 0.95
	pHasPhone       = 0.92
	pSMSOptIn       = 0.70 // among users with a phone
	pEmailOptIn     = 0.79 // among users with an email
	pNeverPurchased = 0.35
	pRecentCart     = 0.55
	pCartConverted  = 0.40 // recent cart followed by a purchase within 48h (buyers only)
	maxOrders       = 40
	minBasketKurus  = 8_000   // 80.00 TRY
	maxBasketKurus  = 150_000 // 1,500.00 TRY
)

// User is one generated profile. Optional values are pointers (nil = NULL).
type User struct {
	FirstName       string
	LastName        string
	Email           *string
	Phone           *string
	City            string
	Language        string
	SMSOptIn        bool
	EmailOptIn      bool
	LastCartAddAt   *time.Time
	LastPurchaseAt  *time.Time
	OrderCount      int
	TotalSpentKurus int64 // money as integer minor units: no float rounding
	CreatedAt       time.Time
}

// Generate returns n users. All timestamps are at or before now, truncated to
// whole seconds and in UTC.
func Generate(n int, seed uint64, now time.Time) []User {
	g := generator{
		rng: rand.New(rand.NewPCG(seed, 0x5eed)),
		now: now.UTC().Truncate(time.Second),
	}
	users := make([]User, n)
	for i := range users {
		users[i] = g.user(i)
	}
	return users
}

type generator struct {
	rng *rand.Rand
	now time.Time
}

func (g *generator) user(i int) User {
	u := User{Language: "tr"}
	if g.chance(pEnglish) {
		u.Language = "en"
		u.FirstName = pick(g.rng, englishFirstNames)
		u.LastName = pick(g.rng, englishLastNames)
	} else {
		if g.chance(0.5) {
			u.FirstName = pick(g.rng, turkishFemaleNames)
		} else {
			u.FirstName = pick(g.rng, turkishMaleNames)
		}
		u.LastName = pick(g.rng, turkishLastNames)
	}
	u.City = g.city()

	if g.chance(pHasEmail) {
		// i+1 keeps addresses unique; example.com is reserved (RFC 2606), so no
		// generated address can belong to a real person.
		e := fmt.Sprintf("%s.%s%d@example.com", Fold(u.FirstName), Fold(u.LastName), i+1)
		u.Email = &e
	}
	if g.chance(pHasPhone) {
		p := fmt.Sprintf("+905%09d", g.rng.IntN(1_000_000_000))
		u.Phone = &p
	}
	u.SMSOptIn = u.Phone != nil && g.chance(pSMSOptIn)
	u.EmailOptIn = u.Email != nil && g.chance(pEmailOptIn)

	if !g.chance(pNeverPurchased) {
		u.OrderCount = min(1+int(g.rng.ExpFloat64()*3), maxOrders)
		for range u.OrderCount {
			u.TotalSpentKurus += minBasketKurus + g.rng.Int64N(maxBasketKurus-minBasketKurus+1)
		}
		t := g.daysAgo(0, 365)
		u.LastPurchaseAt = &t
	}

	if g.chance(pRecentCart) {
		cart := g.daysAgo(0, 90)
		u.LastCartAddAt = &cart
		if u.OrderCount > 0 && g.chance(pCartConverted) {
			p := cart.Add(g.hours(48))
			if p.After(g.now) {
				p = g.now
			}
			u.LastPurchaseAt = &p
		}
	} else if u.LastPurchaseAt != nil {
		// Buyers without recent cart activity added to cart shortly before
		// their last purchase.
		cart := u.LastPurchaseAt.Add(-g.hours(48))
		u.LastCartAddAt = &cart
	}

	earliest := g.now
	for _, t := range []*time.Time{u.LastCartAddAt, u.LastPurchaseAt} {
		if t != nil && t.Before(earliest) {
			earliest = *t
		}
	}
	u.CreatedAt = earliest.Add(-g.duration(1, 700))
	return u
}

func (g *generator) chance(p float64) bool { return g.rng.Float64() < p }

func (g *generator) city() string {
	n := g.rng.IntN(100)
	for _, c := range cityWeights {
		if n < c.Weight {
			return c.City
		}
		n -= c.Weight
	}
	return cityWeights[0].City // unreachable while weights sum to 100
}

// daysAgo returns a time uniformly between minDays and maxDays before now.
func (g *generator) daysAgo(minDays, maxDays float64) time.Time {
	return g.now.Add(-g.duration(minDays, maxDays))
}

func (g *generator) duration(minDays, maxDays float64) time.Duration {
	days := minDays + g.rng.Float64()*(maxDays-minDays)
	return time.Duration(days * float64(24*time.Hour)).Truncate(time.Second)
}

func (g *generator) hours(max float64) time.Duration {
	return time.Duration(g.rng.Float64() * max * float64(time.Hour)).Truncate(time.Second)
}

func pick(rng *rand.Rand, xs []string) string { return xs[rng.IntN(len(xs))] }

// foldReplacer maps Turkish letters to ASCII *before* lower-casing. Lower-casing
// first would be wrong: strings.ToLower("İ") is "i̇" (i + combining dot), not "i".
var foldReplacer = strings.NewReplacer(
	"ç", "c", "Ç", "c", "ğ", "g", "Ğ", "g", "ı", "i", "İ", "i",
	"ö", "o", "Ö", "o", "ş", "s", "Ş", "s", "ü", "u", "Ü", "u",
	" ", "", "-", "", "'", "",
)

// Fold turns a name into an ASCII, lower-case email local part ("Ayşe Gül" → "aysegul").
func Fold(s string) string {
	return strings.ToLower(foldReplacer.Replace(s))
}
