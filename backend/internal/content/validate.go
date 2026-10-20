package content

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrorCode classifies a content problem. Codes are stable so the UI and the
// LLM repair loop can react to them.
type ErrorCode string

const (
	CodeEmpty             ErrorCode = "empty"
	CodeTooLong           ErrorCode = "too_long"
	CodeUnknownVariable   ErrorCode = "unknown_variable"
	CodeMalformedVariable ErrorCode = "malformed_variable"
	CodeMultiline         ErrorCode = "multiline"
	CodeEmoji             ErrorCode = "emoji"
)

// FieldError is one problem in one field ("text", "subject" or "body").
// Messages are written to be actionable by both a person and the model.
type FieldError struct {
	Field   string    `json:"field"`
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Errors is every problem found; validation does not stop at the first.
type Errors []FieldError

func (e Errors) Error() string {
	msgs := make([]string, len(e))
	for i, fe := range e {
		msgs[i] = fe.Message
	}
	return "invalid content: " + strings.Join(msgs, "; ")
}

// FieldLength is the length of one field, counted in Unicode characters.
type FieldLength struct {
	Field string `json:"field"`
	// TemplateLength counts the stored text as written, variables included.
	TemplateLength int `json:"template_length"`
	// WorstCaseLength is the length with every variable replaced by the
	// longest value among the channel's recipients (DECISIONS.md D15). This
	// is the number the limit is checked against.
	WorstCaseLength int `json:"worst_case_length"`
	// Limit is 0 for fields without a limit (the email body).
	Limit int `json:"limit"`
}

// Preview shows a message rendered two ways (DECISIONS.md D22).
type Preview struct {
	// SampleUser is a clearly fictional person, for reading the copy.
	SampleUser Values  `json:"sample_user"`
	Sample     Message `json:"sample"`
	// WorstCase uses the longest values in the audience: the longest message
	// anyone can receive.
	WorstCase Message `json:"worst_case"`
}

// Report is the outcome of validating one channel's message.
type Report struct {
	Valid   bool          `json:"valid"`
	Errors  Errors        `json:"errors"`
	Lengths []FieldLength `json:"lengths"`
	// LongestValues are the values the worst case was computed with.
	LongestValues Values  `json:"longest_values"`
	Preview       Preview `json:"preview"`
}

type field struct {
	name  string
	value string
	limit int
}

func fieldsOf(ch Channel, m Message) []field {
	if ch == Email {
		return []field{{"subject", m.Subject, MaxSubjectRunes}, {"body", m.Body, 0}}
	}
	return []field{{"text", m.Text, MaxSMSRunes}}
}

// Validate checks one channel's message (validation layer 2, DECISIONS.md
// D10). longest holds, for each variable, the longest value among the people
// this channel will reach; a message that fits with those values fits for
// everyone. The same function checks Claude's drafts and the marketer's
// edits.
func Validate(ch Channel, m Message, longest Values) Report {
	worst := worstValues(longest)
	r := Report{Errors: Errors{}, LongestValues: worst}

	for _, f := range fieldsOf(ch, m) {
		_, errs := parse(f.name, f.value)
		rendered := Render(f.value, worst)
		fl := FieldLength{
			Field:           f.name,
			TemplateLength:  utf8.RuneCountInString(f.value),
			WorstCaseLength: utf8.RuneCountInString(rendered),
			Limit:           f.limit,
		}
		r.Lengths = append(r.Lengths, fl)

		if strings.TrimSpace(f.value) == "" {
			r.Errors = append(r.Errors, FieldError{Field: f.name, Code: CodeEmpty, Message: f.name + " is empty"})
			continue
		}
		r.Errors = append(r.Errors, errs...)
		if found := emojiIn(f.value); len(found) > 0 {
			r.Errors = append(r.Errors, FieldError{Field: f.name, Code: CodeEmoji,
				Message: fmt.Sprintf("%s contains emoji (%s); write it without emoji or other pictographic symbols", f.name, strings.Join(found, ", "))})
		}
		if f.name == "subject" && strings.ContainsAny(f.value, "\r\n") {
			r.Errors = append(r.Errors, FieldError{Field: f.name, Code: CodeMultiline,
				Message: "subject must be a single line"})
		}
		if f.limit > 0 && fl.WorstCaseLength > f.limit {
			r.Errors = append(r.Errors, FieldError{Field: f.name, Code: CodeTooLong,
				Message: tooLongMessage(f, fl, worst)})
		}
	}

	sample := SampleUser(longest)
	r.Preview = Preview{
		SampleUser: sample,
		Sample:     RenderMessage(m, sample),
		WorstCase:  RenderMessage(m, worst),
	}
	r.Valid = len(r.Errors) == 0
	return r
}

func tooLongMessage(f field, fl FieldLength, worst Values) string {
	var used []string
	toks, _ := parse(f.name, f.value)
	seen := map[Variable]bool{}
	for _, t := range toks {
		if t.v != "" && !seen[t.v] {
			seen[t.v] = true
			used = append(used, fmt.Sprintf("%s = %q, %d characters", t.v.Placeholder(), worst[t.v], utf8.RuneCountInString(worst[t.v])))
		}
	}
	msg := fmt.Sprintf("%s is %d characters", f.name, fl.WorstCaseLength)
	if len(used) > 0 {
		msg += " with the longest values in this audience filled in (" + strings.Join(used, "; ") + ")"
	}
	return msg + fmt.Sprintf("; the limit is %d. Shorten it by at least %d characters.", f.limit, fl.WorstCaseLength-f.limit)
}

// isEmoji reports whether r is an emoji or another pictographic symbol.
// Messages carry none for now (DECISIONS.md D15): on SMS most emoji take two
// UTF-16 units while our counter counts one, phones and mail clients show
// them unevenly, and the brief never asks for them. The list is explicit:
// everything outside the Basic Multilingual Plane (where almost all emoji
// live), the Miscellaneous Symbols and Dingbats blocks (☀ ✨ ✔), the
// Miscellaneous Symbols and Arrows block (⭐), and the invisible joiners
// that build emoji sequences. Turkish letters, %, ₺, © and ™ are not in it.
func isEmoji(r rune) bool {
	switch {
	case r > 0xFFFF:
		return true
	case r >= 0x2600 && r <= 0x27BF, r >= 0x2B00 && r <= 0x2BFF:
		return true
	case r == 0xFE0F, r == 0x200D, r == 0x20E3: // variation selector, zero-width joiner, keycap
		return true
	}
	return false
}

// emojiIn lists the distinct emoji in s, quoted; an invisible joiner is
// shown by its code point so the message says what to delete.
func emojiIn(s string) []string {
	var found []string
	seen := map[rune]bool{}
	for _, r := range s {
		if !isEmoji(r) || seen[r] {
			continue
		}
		seen[r] = true
		if r == 0xFE0F || r == 0x200D || r == 0x20E3 {
			found = append(found, fmt.Sprintf("U+%04X", r))
		} else {
			found = append(found, fmt.Sprintf("%q", string(r)))
		}
	}
	return found
}

// worstValues picks, for each variable, the longer of the audience's longest
// value and the fallback that an empty value would render as.
func worstValues(longest Values) Values {
	w := make(Values, len(Variables))
	for _, v := range Variables {
		w[v] = longest[v]
		if utf8.RuneCountInString(Fallbacks[v]) > utf8.RuneCountInString(w[v]) {
			w[v] = Fallbacks[v]
		}
	}
	return w
}

// Longest returns, for each variable, the longest value among vs, counted in
// characters; ties go to the value that sorts first, so the result is
// deterministic. It measures a set of recipients already in hand (the
// approval) the way segment.Counter.LongestValues measures an audience in
// the database.
func Longest(vs []Values) Values {
	out := Values{}
	for _, v := range Variables {
		best, bestLen := "", 0
		for _, vals := range vs {
			s := vals[v]
			n := utf8.RuneCountInString(s)
			if n > bestLen || (n == bestLen && s < best) {
				best, bestLen = s, n
			}
		}
		out[v] = best
	}
	return out
}

// SampleUser is the fictional person previews are rendered for. The city is
// taken from the audience so the preview reads naturally.
func SampleUser(longest Values) Values {
	city := longest[City]
	if city == "" {
		city = "İstanbul"
	}
	return Values{FirstName: "Ayşe", LastName: "Yılmaz", City: city}
}
