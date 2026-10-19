package llm

import (
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// PartialStrings reads the top-level string fields of a JSON object that may
// still be arriving (DECISIONS.md D11). For every field it has seen it
// returns the value decoded so far; an unfinished escape sequence at the end
// is held back until it is complete. It never returns text that is not in
// the input, and for a complete object it returns exactly what
// encoding/json would. Values that are not strings are skipped.
func PartialStrings(s string) map[string]string {
	out := map[string]string{}
	p := &partial{s: s}
	p.ws()
	if !p.eat('{') {
		return out
	}
	for {
		p.ws()
		if p.eat('}') || p.done() {
			return out
		}
		p.eat(',')
		p.ws()
		key, ok := p.str()
		if !ok {
			return out
		}
		p.ws()
		if !p.eat(':') {
			return out
		}
		p.ws()
		if p.done() {
			return out
		}
		if p.peek() != '"' {
			if !p.skipValue() {
				return out
			}
			continue
		}
		val, complete := p.str()
		out[key] = val
		if !complete {
			return out
		}
	}
}

type partial struct {
	s string
	i int
}

func (p *partial) done() bool { return p.i >= len(p.s) }
func (p *partial) peek() byte { return p.s[p.i] }
func (p *partial) ws() {
	for !p.done() && (p.s[p.i] == ' ' || p.s[p.i] == '\n' || p.s[p.i] == '\r' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *partial) eat(c byte) bool {
	if !p.done() && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

// str decodes a JSON string starting at the opening quote. It returns what
// was decoded and whether the closing quote was reached.
func (p *partial) str() (string, bool) {
	if !p.eat('"') {
		return "", false
	}
	var b []byte
	for !p.done() {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return string(b), true
		case c != '\\':
			// Copy one whole UTF-8 sequence, or stop if it is cut off.
			r, size := utf8.DecodeRuneInString(p.s[p.i:])
			if r == utf8.RuneError && size <= 1 {
				if !utf8.FullRuneInString(p.s[p.i:]) {
					return string(b), false
				}
				b = utf8.AppendRune(b, utf8.RuneError) // invalid byte, as encoding/json does
				p.i++
				continue
			}
			b = append(b, p.s[p.i:p.i+size]...)
			p.i += size
		default:
			r, n, ok := p.escape(p.s[p.i:])
			if !ok {
				return string(b), false // unfinished escape: hold it back
			}
			b = utf8.AppendRune(b, r)
			p.i += n
		}
	}
	return string(b), false
}

// escape decodes one escape sequence (and a following low surrogate for a
// \u high surrogate). ok is false when the input ends before it is complete.
func (p *partial) escape(s string) (r rune, n int, ok bool) {
	if len(s) < 2 {
		return 0, 0, false
	}
	switch s[1] {
	case '"':
		return '"', 2, true
	case '\\':
		return '\\', 2, true
	case '/':
		return '/', 2, true
	case 'b':
		return '\b', 2, true
	case 'f':
		return '\f', 2, true
	case 'n':
		return '\n', 2, true
	case 'r':
		return '\r', 2, true
	case 't':
		return '\t', 2, true
	case 'u':
		if len(s) < 6 {
			return 0, 0, false
		}
		v, err := strconv.ParseUint(s[2:6], 16, 16)
		if err != nil {
			return utf8.RuneError, 6, true
		}
		r := rune(v)
		if !utf16.IsSurrogate(r) {
			return r, 6, true
		}
		// A high surrogate needs its low half: 😀.
		if len(s) < 12 {
			if len(s) >= 7 && s[6] != '\\' || len(s) >= 8 && s[7] != 'u' {
				return utf8.RuneError, 6, true
			}
			return 0, 0, false
		}
		if s[6] != '\\' || s[7] != 'u' {
			return utf8.RuneError, 6, true
		}
		lo, err := strconv.ParseUint(s[8:12], 16, 16)
		if err != nil {
			return utf8.RuneError, 6, true
		}
		if dec := utf16.DecodeRune(r, rune(lo)); dec != utf8.RuneError {
			return dec, 12, true
		}
		return utf8.RuneError, 6, true
	}
	return utf8.RuneError, 2, true
}

// skipValue skips a non-string value (number, literal, array or object).
// It reports false when the value is not finished yet.
func (p *partial) skipValue() bool {
	depth := 0
	for !p.done() {
		c := p.s[p.i]
		switch c {
		case '"':
			if _, complete := p.str(); !complete {
				return false
			}
			continue
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return true // the enclosing object's end
			}
			depth--
		case ',':
			if depth == 0 {
				return true
			}
		}
		p.i++
	}
	return false
}
