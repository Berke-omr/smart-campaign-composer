package segment

import (
	"fmt"
	"strconv"
	"strings"
)

// Description is a deterministic, human-readable rendering of a validated
// rule. The UI shows this rather than the LLM's own explanation, so what the
// marketer reads is exactly what will run.
type Description struct {
	Summary string   `json:"summary"`
	Lines   []string `json:"lines"`
}

var enumLabels = map[string]map[string]string{
	"language": {"tr": "Turkish", "en": "English"},
}

// Describe renders a validated rule, e.g.
// "City is İstanbul", "Last purchase: not within the last 30 days (or never)".
func Describe(v Validated) Description {
	if len(v.conditions) == 0 {
		return Description{Summary: "All users", Lines: []string{}}
	}
	d := Description{Summary: "Users matching all of the following"}
	if v.match == MatchAny {
		d.Summary = "Users matching any of the following"
	}
	for _, c := range v.conditions {
		d.Lines = append(d.Lines, describe(c))
	}
	return d
}

func describe(c validCondition) string {
	f := c.field
	switch c.op {
	case OpEq:
		return fmt.Sprintf("%s is %s", f.Label, scalarText(c))
	case OpNeq:
		return fmt.Sprintf("%s is not %s", f.Label, scalarText(c))
	case OpIn:
		return fmt.Sprintf("%s is one of %s", f.Label, listText(c))
	case OpNotIn:
		return fmt.Sprintf("%s is none of %s", f.Label, listText(c))
	case OpGt:
		return fmt.Sprintf("%s is more than %s", f.Label, numText(c.nums[0]))
	case OpGte:
		return fmt.Sprintf("%s is at least %s", f.Label, numText(c.nums[0]))
	case OpLt:
		return fmt.Sprintf("%s is less than %s", f.Label, numText(c.nums[0]))
	case OpLte:
		return fmt.Sprintf("%s is at most %s", f.Label, numText(c.nums[0]))
	case OpBetween:
		return fmt.Sprintf("%s is between %s and %s", f.Label, numText(c.nums[0]), numText(c.nums[1]))
	case OpWithinLastDays:
		return fmt.Sprintf("%s: within the last %s", f.Label, daysText(c.days))
	case OpNotWithinLastDays:
		s := fmt.Sprintf("%s: not within the last %s", f.Label, daysText(c.days))
		if f.Nullable {
			s += " (or never)"
		}
		return s
	case OpIsNull:
		return f.Label + ": never"
	case OpIsNotNull:
		return f.Label + ": at some point"
	}
	return f.Label + " " + string(c.op)
}

func scalarText(c validCondition) string {
	if c.strs != nil {
		return enumText(c.field.Name, c.strs[0])
	}
	return numText(c.nums[0])
}

func listText(c validCondition) string {
	parts := make([]string, len(c.strs))
	for i, s := range c.strs {
		parts[i] = enumText(c.field.Name, s)
	}
	return strings.Join(parts, ", ")
}

func enumText(field, value string) string {
	if label, ok := enumLabels[field][value]; ok {
		return label
	}
	return value
}

func numText(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func daysText(days int) string {
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}
