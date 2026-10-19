package content

import (
	"fmt"
	"strings"
)

// Fallbacks replace an empty value when a message is rendered. The users
// table forbids empty names and cities, so they are a defensive default;
// the worst-case length still accounts for them.
var Fallbacks = Values{
	FirstName: "Müşterimiz",
	LastName:  "",
	City:      "",
}

// token is one piece of a parsed template: literal text or a variable.
type token struct {
	text string   // literal text (when variable is empty)
	v    Variable // the variable, for {{name}} tokens
}

// parse splits a template into literal text and whitelisted variables. It
// reports every malformed or unknown variable instead of stopping at the
// first, so one repair round can fix them all.
func parse(field, template string) ([]token, Errors) {
	var toks []token
	var errs Errors
	rest := template
	for rest != "" {
		open := strings.Index(rest, "{{")
		closeBeforeOpen := strings.Index(rest, "}}")
		if closeBeforeOpen >= 0 && (open < 0 || closeBeforeOpen < open) {
			errs = append(errs, FieldError{Field: field, Code: CodeMalformedVariable,
				Message: fmt.Sprintf(`%s has "}}" without a matching "{{"; write variables exactly as %s`, field, allowedList())})
			toks = append(toks, token{text: rest[:closeBeforeOpen+2]})
			rest = rest[closeBeforeOpen+2:]
			continue
		}
		if open < 0 {
			toks = append(toks, token{text: rest})
			break
		}
		if open > 0 {
			toks = append(toks, token{text: rest[:open]})
		}
		end := strings.Index(rest[open+2:], "}}")
		if end < 0 {
			errs = append(errs, FieldError{Field: field, Code: CodeMalformedVariable,
				Message: fmt.Sprintf(`%s has "{{" that is never closed; write variables exactly as %s`, field, allowedList())})
			toks = append(toks, token{text: rest[open:]})
			break
		}
		name := rest[open+2 : open+2+end]
		raw := rest[open : open+2+end+2]
		rest = rest[open+2+end+2:]

		switch v, ok := lookupVariable(name); {
		case ok:
			toks = append(toks, token{v: v})
		case isVariableLike(strings.TrimSpace(name)):
			if tv, ok := lookupVariable(strings.TrimSpace(name)); ok {
				errs = append(errs, FieldError{Field: field, Code: CodeMalformedVariable,
					Message: fmt.Sprintf(`%s has %q; write it exactly as %s, without spaces`, field, raw, tv.Placeholder())})
			} else {
				errs = append(errs, FieldError{Field: field, Code: CodeUnknownVariable,
					Message: fmt.Sprintf("%s uses unknown variable %s; allowed: %s", field, raw, allowedList())})
			}
			toks = append(toks, token{text: raw})
		default:
			errs = append(errs, FieldError{Field: field, Code: CodeMalformedVariable,
				Message: fmt.Sprintf("%s has %q, which is not a variable; allowed: %s", field, raw, allowedList())})
			toks = append(toks, token{text: raw})
		}
	}
	return toks, errs
}

func lookupVariable(name string) (Variable, bool) {
	for _, v := range Variables {
		if string(v) == name {
			return v, true
		}
	}
	return "", false
}

// isVariableLike reports whether s looks like an identifier (letters,
// digits, underscores), i.e. an attempt at a variable name.
func isVariableLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func allowedList() string {
	s := make([]string, len(Variables))
	for i, v := range Variables {
		s[i] = v.Placeholder()
	}
	return strings.Join(s, ", ")
}

// Render fills a template's variables from values (empty values use
// Fallbacks). Text that is not a valid variable is kept as written, so a
// preview of an invalid draft shows exactly what is wrong.
func Render(template string, values Values) string {
	toks, _ := parse("", template)
	var b strings.Builder
	for _, t := range toks {
		if t.v == "" {
			b.WriteString(t.text)
			continue
		}
		val := values[t.v]
		if val == "" {
			val = Fallbacks[t.v]
		}
		b.WriteString(val)
	}
	return b.String()
}

// RenderMessage renders every field of a message.
func RenderMessage(m Message, values Values) Message {
	return Message{
		Text:    Render(m.Text, values),
		Subject: Render(m.Subject, values),
		Body:    Render(m.Body, values),
	}
}
