package segment

import (
	"strconv"
	"strings"
	"time"
)

// Query is a boolean SQL expression over the users table with $1..$n
// placeholders, plus the values for them.
type Query struct {
	Where string
	Args  []any
}

// Compile turns a validated rule into a parameterized WHERE expression.
//
// asOf anchors the relative-time operators: "within the last N days" means
// column >= asOf - N days. Passing it in (instead of using SQL now()) makes
// results reproducible and lets a preview and the later approval count the
// same audience.
//
// Every identifier in the output comes from the registry and every value is a
// bind parameter; nothing from the rule is concatenated into the SQL text.
func Compile(v Validated, asOf time.Time) Query {
	if len(v.conditions) == 0 {
		return Query{Where: "TRUE"}
	}
	b := &builder{}
	parts := make([]string, len(v.conditions))
	for i, c := range v.conditions {
		parts[i] = b.condition(c, asOf.UTC())
	}
	joiner := " AND "
	if v.match == MatchAny {
		joiner = " OR "
	}
	return Query{Where: "(" + strings.Join(parts, joiner) + ")", Args: b.args}
}

type builder struct {
	args []any
}

// arg registers a bind value and returns its placeholder.
func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *builder) condition(c validCondition, asOf time.Time) string {
	col := c.field.column
	switch c.op {
	case OpEq:
		return col + " = " + b.arg(b.scalar(c))
	case OpNeq:
		return col + " <> " + b.arg(b.scalar(c))
	case OpIn:
		return col + " = ANY(" + b.arg(c.strs) + ")"
	case OpNotIn:
		return col + " <> ALL(" + b.arg(c.strs) + ")"
	case OpGt:
		return col + " > " + b.arg(b.scalar(c))
	case OpGte:
		return col + " >= " + b.arg(b.scalar(c))
	case OpLt:
		return col + " < " + b.arg(b.scalar(c))
	case OpLte:
		return col + " <= " + b.arg(b.scalar(c))
	case OpBetween:
		lo, hi := b.number(c.field, c.nums[0]), b.number(c.field, c.nums[1])
		return col + " BETWEEN " + b.arg(lo) + " AND " + b.arg(hi)
	case OpWithinLastDays:
		// NULL (never happened) does not match: it did not happen recently.
		return col + " >= " + b.arg(cutoff(asOf, c.days))
	case OpNotWithinLastDays:
		// NULL (never happened) matches: it did not happen recently either.
		if c.field.Nullable {
			return "(" + col + " IS NULL OR " + col + " < " + b.arg(cutoff(asOf, c.days)) + ")"
		}
		return col + " < " + b.arg(cutoff(asOf, c.days))
	case OpIsNull:
		return col + " IS NULL"
	case OpIsNotNull:
		return col + " IS NOT NULL"
	}
	// Unreachable: Validate only lets registered operators through.
	panic("segment: unhandled operator " + string(c.op))
}

func (b *builder) scalar(c validCondition) any {
	if c.field.Type == TypeEnum {
		return c.strs[0]
	}
	return b.number(c.field, c.nums[0])
}

// number passes integers as int64 so Postgres compares integer columns
// with integers, and keeps float64 for numeric (money) columns.
func (b *builder) number(f *Field, n float64) any {
	if f.Type == TypeInteger {
		return int64(n)
	}
	return n
}

func cutoff(asOf time.Time, days int) time.Time {
	return asOf.AddDate(0, 0, -days)
}
