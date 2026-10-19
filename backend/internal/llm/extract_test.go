package llm

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestPartialStrings(t *testing.T) {
	tests := []struct {
		in   string
		want map[string]string
	}{
		{``, map[string]string{}},
		{`{`, map[string]string{}},
		{`{"subj`, map[string]string{}},
		{`{"subject"`, map[string]string{}},
		{`{"subject":`, map[string]string{}},
		{`{"subject":"`, map[string]string{"subject": ""}},
		{`{"subject":"Merhaba {{first_`, map[string]string{"subject": "Merhaba {{first_"}},
		{`{"subject":"a\`, map[string]string{"subject": "a"}},       // escape held back
		{`{"subject":"a\n`, map[string]string{"subject": "a\n"}},    // complete escape
		{`{"subject":"a\u01`, map[string]string{"subject": "a"}},    // \u held back
		{`{"subject":"a\u0130`, map[string]string{"subject": "aİ"}}, // \u0130 = İ
		{`{"subject":"a\ud83d`, map[string]string{"subject": "a"}},  // high surrogate waits
		{`{"subject":"a\ud83d\ude00`, map[string]string{"subject": "a😀"}},
		{"{\"subject\":\"ş\xc5", map[string]string{"subject": "ş"}}, // cut inside a UTF-8 sequence
		{`{"subject":"Konu","body":"Merhaba,\n\nSe`, map[string]string{"subject": "Konu", "body": "Merhaba,\n\nSe"}},
		{`{ "n" : 12 , "text" : "x" }`, map[string]string{"text": "x"}}, // non-string skipped
		{`{"a":[1,{"b":"}"}],"text":"ok"}`, map[string]string{"text": "ok"}},
		{`{"text":"say \"hi\""}`, map[string]string{"text": `say "hi"`}},
	}
	for _, tt := range tests {
		got := PartialStrings(tt.in)
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("PartialStrings(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// asciiJSON encodes s as a JSON string with every non-ASCII rune escaped
// (\uXXXX, surrogate pairs above U+FFFF), the form the API may also send.
func asciiJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 || r > 0x7e:
			if r > 0xffff {
				hi, lo := utf16.EncodeRune(r)
				fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Property: for every prefix of a streamed answer, each value is a prefix of
// the final value, and the complete answer decodes exactly like
// encoding/json. So the UI never shows text the model did not write.
func TestPartialStringsOnEveryPrefix(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	alphabet := []rune("aşğİıöüç {}\"\\\n\t/%😀{{first_name}}—")
	for i := 0; i < 300; i++ {
		obj := map[string]string{}
		keys := []string{"subject", "body"}
		for _, k := range keys {
			n := rng.IntN(40)
			r := make([]rune, n)
			for j := range r {
				r[j] = alphabet[rng.IntN(len(alphabet))]
			}
			obj[k] = string(r)
		}
		var raw string
		if i%2 == 0 {
			b, _ := json.Marshal(obj) // UTF-8 as is, keys sorted
			raw = string(b)
		} else {
			raw = `{"subject":` + asciiJSON(obj["subject"]) + `, "body":` + asciiJSON(obj["body"]) + `}`
		}

		var want map[string]string
		if err := json.Unmarshal([]byte(raw), &want); err != nil {
			t.Fatal(err)
		}
		if got := PartialStrings(raw); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("complete %s:\n got %q\nwant %q", raw, got, want)
		}
		for end := 0; end <= len(raw); end++ {
			for k, v := range PartialStrings(raw[:end]) {
				if !strings.HasPrefix(want[k], v) {
					t.Fatalf("prefix %q: %s = %q is not a prefix of %q", raw[:end], k, v, want[k])
				}
			}
		}
	}
}

func FuzzPartialStrings(f *testing.F) {
	for _, s := range []string{`{"text":"a"}`, `{"subject":"\u0130","body":"x\ny"}`, `{"text":"\ud83d\ude00"}`, `{"a":[{"b":"]"}],"text":""}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := PartialStrings(s) // must never panic
		var want map[string]any
		if json.Unmarshal([]byte(s), &want) != nil {
			return
		}
		for k, v := range want {
			if str, ok := v.(string); ok && strings.ToValidUTF8(str, "\uFFFD") == str {
				if got[k] != str {
					t.Fatalf("%s: %s = %q, encoding/json says %q", s, k, got[k], str)
				}
			}
		}
	})
}
