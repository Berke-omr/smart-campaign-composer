package segment

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
)

// genCondition is a randomly generated condition in typed form. The oracle in
// audience_test.go evaluates it directly, independently of Validate/Compile.
type genCondition struct {
	field string
	op    Op
	strs  []string
	nums  []float64
	days  int
}

type genRule struct {
	match Match
	conds []genCondition
}

// randomRule returns a valid rule as JSON together with its typed form.
// Value ranges are chosen to be interesting for the seeded data (e.g. days up
// to 120, small order counts) so results are neither always empty nor always
// everyone.
func randomRule(rng *rand.Rand, reg *Registry) (json.RawMessage, genRule) {
	g := genRule{match: MatchAll}
	if rng.IntN(2) == 0 {
		g.match = MatchAny
	}
	fields := reg.Fields()
	n := rng.IntN(5) // 0..4 conditions
	for range n {
		f := fields[rng.IntN(len(fields))]
		op := f.Ops[rng.IntN(len(f.Ops))]
		c := genCondition{field: f.Name, op: op}
		switch op.shape() {
		case shapeNone:
		case shapeDays:
			c.days = 1 + rng.IntN(120)
		case shapeList:
			k := 1 + rng.IntN(min(3, len(f.Enum)))
			perm := rng.Perm(len(f.Enum))[:k]
			for _, i := range perm {
				c.strs = append(c.strs, f.Enum[i])
			}
		case shapeScalar:
			if f.Type == TypeEnum {
				c.strs = []string{f.Enum[rng.IntN(len(f.Enum))]}
			} else {
				c.nums = []float64{randomNumber(rng, f)}
			}
		case shapePair:
			a, b := randomNumber(rng, f), randomNumber(rng, f)
			c.nums = []float64{min(a, b), max(a, b)}
		}
		g.conds = append(g.conds, c)
	}
	return g.json(), g
}

// randomNumber picks whole numbers only, so money comparisons never sit on a
// floating-point boundary.
func randomNumber(rng *rand.Rand, f Field) float64 {
	if f.Name == "order_count" {
		return float64(rng.IntN(8))
	}
	return float64(rng.IntN(20_000))
}

func (g genRule) json() json.RawMessage {
	type cond struct {
		Field string `json:"field"`
		Op    Op     `json:"op"`
		Value any    `json:"value,omitempty"`
	}
	out := struct {
		Match      Match  `json:"match"`
		Conditions []cond `json:"conditions"`
	}{Match: g.match, Conditions: []cond{}}
	for _, c := range g.conds {
		var v any
		switch c.op.shape() {
		case shapeDays:
			v = c.days
		case shapeList:
			v = slices.Clone(c.strs)
		case shapeScalar:
			if c.strs != nil {
				v = c.strs[0]
			} else {
				v = c.nums[0]
			}
		case shapePair:
			v = slices.Clone(c.nums)
		}
		out.Conditions = append(out.Conditions, cond{Field: c.field, Op: c.op, Value: v})
	}
	b, err := json.Marshal(out)
	if err != nil {
		panic(err)
	}
	return b
}
