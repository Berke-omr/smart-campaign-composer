package content

import (
	"maps"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// The longest values in the case's sample audience (measured on the seed).
var sampleLongest = Values{FirstName: "Muhammed Emin", LastName: "Montgomery-Fitzgerald", City: "İstanbul"}

func codes(r Report) []ErrorCode {
	var c []ErrorCode
	for _, e := range r.Errors {
		c = append(c, e.Code)
	}
	return c
}

func hasCode(r Report, field string, code ErrorCode) bool {
	for _, e := range r.Errors {
		if e.Field == field && e.Code == code {
			return true
		}
	}
	return false
}

func TestSMSLimitIsCountedInCharactersNotBytes(t *testing.T) {
	// "ş" is 2 bytes in UTF-8. 160 of them is 320 bytes but 160 characters.
	at := strings.Repeat("ş", MaxSMSRunes)
	if r := Validate(SMS, Message{Text: at}, sampleLongest); !r.Valid {
		t.Errorf("160 Turkish characters rejected: %v", r.Errors)
	}
	over := at + "ğ"
	r := Validate(SMS, Message{Text: over}, sampleLongest)
	if !hasCode(r, "text", CodeTooLong) {
		t.Fatalf("161 characters accepted: %+v", r)
	}
	if r.Lengths[0].TemplateLength != 161 || r.Lengths[0].WorstCaseLength != 161 || r.Lengths[0].Limit != 160 {
		t.Errorf("lengths = %+v", r.Lengths[0])
	}
}

func TestSubjectLimit(t *testing.T) {
	if r := Validate(Email, Message{Subject: strings.Repeat("İ", 80), Body: "b"}, sampleLongest); !r.Valid {
		t.Errorf("80-character subject rejected: %v", r.Errors)
	}
	if r := Validate(Email, Message{Subject: strings.Repeat("İ", 81), Body: "b"}, sampleLongest); !hasCode(r, "subject", CodeTooLong) {
		t.Errorf("81-character subject accepted: %v", codes(r))
	}
	// The body has no limit.
	if r := Validate(Email, Message{Subject: "s", Body: strings.Repeat("ö", 5000)}, sampleLongest); !r.Valid {
		t.Errorf("long body rejected: %v", r.Errors)
	}
}

func TestWorstCaseUsesTheLongestValues(t *testing.T) {
	// 147 literal characters + {{first_name}}: the template is 161 characters
	// long but renders to 147 + 13 = 160 with the longest name. Valid.
	literal := strings.Repeat("a", 147)
	r := Validate(SMS, Message{Text: literal + "{{first_name}}"}, sampleLongest)
	if !r.Valid || r.Lengths[0].TemplateLength != 161 || r.Lengths[0].WorstCaseLength != 160 {
		t.Errorf("template counted instead of the rendered worst case: %+v %v", r.Lengths, r.Errors)
	}

	// One character more and the longest name no longer fits.
	r = Validate(SMS, Message{Text: literal + "a{{first_name}}"}, sampleLongest)
	if !hasCode(r, "text", CodeTooLong) {
		t.Fatalf("161-character worst case accepted: %+v", r.Lengths)
	}
	msg := r.Errors[0].Message
	for _, want := range []string{"161 characters", `{{first_name}} = "Muhammed Emin", 13 characters`, "limit is 160", "at least 1 characters"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}

	// A short template with the 21-character surname twice.
	r = Validate(Email, Message{Subject: strings.Repeat("x", 40) + "{{last_name}} {{last_name}}", Body: "b"}, sampleLongest)
	if r.Lengths[0].WorstCaseLength != 40+21+1+21 || !hasCode(r, "subject", CodeTooLong) {
		t.Errorf("repeated variable: %+v %v", r.Lengths[0], codes(r))
	}
}

func TestWorstCaseCoversFallbacks(t *testing.T) {
	// With no data for a variable, the fallback is what gets rendered.
	r := Validate(SMS, Message{Text: "Merhaba {{first_name}}"}, Values{})
	want := utf8.RuneCountInString("Merhaba " + Fallbacks[FirstName])
	if r.Lengths[0].WorstCaseLength != want {
		t.Errorf("worst case = %d, want %d (fallback)", r.Lengths[0].WorstCaseLength, want)
	}
}

func TestVariables(t *testing.T) {
	tests := []struct {
		text string
		want []ErrorCode
	}{
		{"Merhaba {{first_name}} {{last_name}}, {{city}} için", nil},
		{"{{first_name}}{{first_name}}", nil},
		{"Merhaba {{name}}", []ErrorCode{CodeUnknownVariable}},
		{"Merhaba {{ first_name }}", []ErrorCode{CodeMalformedVariable}},
		{"Merhaba {{First_Name}}", []ErrorCode{CodeUnknownVariable}},
		{"Merhaba {{first_name}", []ErrorCode{CodeMalformedVariable}},
		{"Merhaba first_name}}", []ErrorCode{CodeMalformedVariable}},
		{"İndirim {{%30}}", []ErrorCode{CodeMalformedVariable}},
		{"{{a}} ve {{b}}", []ErrorCode{CodeUnknownVariable, CodeUnknownVariable}},
		{"Tek parantez {first_name} değişken değildir", nil},
	}
	for _, tt := range tests {
		r := Validate(SMS, Message{Text: tt.text}, sampleLongest)
		got := codes(r)
		if len(got) != len(tt.want) {
			t.Errorf("%q: codes = %v, want %v", tt.text, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%q: codes = %v, want %v", tt.text, got, tt.want)
			}
		}
	}

	r := Validate(SMS, Message{Text: "Merhaba {{ first_name }}"}, sampleLongest)
	if !strings.Contains(r.Errors[0].Message, "{{first_name}}") {
		t.Errorf("malformed message should show the right spelling: %q", r.Errors[0].Message)
	}
	r = Validate(SMS, Message{Text: "Merhaba {{name}}"}, sampleLongest)
	if !strings.Contains(r.Errors[0].Message, "{{first_name}}, {{last_name}}, {{city}}") {
		t.Errorf("unknown variable message should list the allowed ones: %q", r.Errors[0].Message)
	}
}

func TestEmptyAndMultiline(t *testing.T) {
	r := Validate(Email, Message{Subject: "  ", Body: ""}, sampleLongest)
	if !hasCode(r, "subject", CodeEmpty) || !hasCode(r, "body", CodeEmpty) {
		t.Errorf("empty fields: %v", r.Errors)
	}
	if r := Validate(SMS, Message{}, sampleLongest); !hasCode(r, "text", CodeEmpty) {
		t.Errorf("empty SMS: %v", r.Errors)
	}
	// A line break in a subject would end up in a mail header.
	if r := Validate(Email, Message{Subject: "Merhaba\nBcc: x@example.com", Body: "b"}, sampleLongest); !hasCode(r, "subject", CodeMultiline) {
		t.Errorf("multi-line subject accepted: %v", r.Errors)
	}
	// Line breaks are fine in the body.
	if r := Validate(Email, Message{Subject: "s", Body: "Merhaba,\n\nGörüşmek üzere"}, sampleLongest); !r.Valid {
		t.Errorf("multi-line body rejected: %v", r.Errors)
	}
}

func TestAllProblemsAreReportedTogether(t *testing.T) {
	r := Validate(Email, Message{Subject: strings.Repeat("x", 90) + "{{foo}}", Body: "{{ city }}"}, sampleLongest)
	for _, want := range []struct {
		field string
		code  ErrorCode
	}{{"subject", CodeUnknownVariable}, {"subject", CodeTooLong}, {"body", CodeMalformedVariable}} {
		if !hasCode(r, want.field, want.code) {
			t.Errorf("missing %s/%s in %v", want.field, want.code, r.Errors)
		}
	}
}

func TestPreview(t *testing.T) {
	m := Message{Subject: "{{first_name}}, sepetin seni bekliyor", Body: "Merhaba {{first_name}} {{last_name}},\n\n{{city}} için %30 indirim."}
	r := Validate(Email, m, sampleLongest)
	if r.Preview.Sample.Subject != "Ayşe, sepetin seni bekliyor" {
		t.Errorf("sample subject = %q", r.Preview.Sample.Subject)
	}
	if r.Preview.WorstCase.Body != "Merhaba Muhammed Emin Montgomery-Fitzgerald,\n\nİstanbul için %30 indirim." {
		t.Errorf("worst-case body = %q", r.Preview.WorstCase.Body)
	}
	if r.Preview.SampleUser[City] != "İstanbul" || r.LongestValues[LastName] != "Montgomery-Fitzgerald" {
		t.Errorf("preview values = %v / %v", r.Preview.SampleUser, r.LongestValues)
	}
	// The stored template is never changed by rendering.
	if m.Subject != "{{first_name}}, sepetin seni bekliyor" {
		t.Error("template modified")
	}
}

func TestRenderKeepsInvalidTextAsWritten(t *testing.T) {
	got := Render("Merhaba {{name}} {{first_name}} {{", Values{FirstName: "Ece"})
	if got != "Merhaba {{name}} Ece {{" {
		t.Errorf("Render = %q", got)
	}
}

// Property: if Validate accepts a template for an audience, every rendering
// with values no longer than that audience's longest ones fits the limit.
// This is the guarantee the worst-case check exists for.
func TestValidTemplatesFitForEveryone(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	pieces := []string{"a", "ş", "İ", " ", "%30", "!", "{{first_name}}", "{{last_name}}", "{{city}}", "ğü"}
	accepted := 0
	for i := 0; i < 3000; i++ {
		var b strings.Builder
		for n := rng.IntN(40); n > 0; n-- {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		text := b.String()
		if !Validate(SMS, Message{Text: text}, sampleLongest).Valid {
			continue
		}
		accepted++
		for j := 0; j < 5; j++ {
			person := Values{}
			for v, longest := range sampleLongest {
				runes := []rune(longest)
				person[v] = string(runes[:1+rng.IntN(len(runes))]) // a prefix: never longer
			}
			if n := utf8.RuneCountInString(Render(text, person)); n > MaxSMSRunes {
				t.Fatalf("accepted %q renders to %d characters for %v", text, n, person)
			}
		}
	}
	if accepted < 300 {
		t.Fatalf("only %d of 3000 random templates were valid; the property was barely tested", accepted)
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{"", "{{first_name}}", "{{", "}}", "{{{first_name}}}", "{{ city }}", "a{{b}}c", "ş{{last_name}}ğ"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r := Validate(SMS, Message{Text: s}, sampleLongest)
		if r.Valid && utf8.RuneCountInString(Render(s, sampleLongest)) > MaxSMSRunes {
			t.Fatalf("valid but too long: %q", s)
		}
		if r.Valid && strings.Contains(Render(s, sampleLongest), "{{") {
			t.Fatalf("valid template rendered with a leftover placeholder: %q", s)
		}
	})
}

func TestLongest(t *testing.T) {
	got := Longest([]Values{
		{FirstName: "Ayşe", LastName: "Yılmaz", City: "İzmir"},
		{FirstName: "Muhammed Emin", LastName: "Öz", City: "İstanbul"},
		{FirstName: "ŞÖĞÜÇŞÖĞÜÇ", LastName: "Kaya", City: "Ankara"},
	})
	// Characters, not bytes: "ŞÖĞÜÇŞÖĞÜÇ" is 10 characters but 20 bytes, so
	// it loses to "Muhammed Emin" (13 of each). Ties go to the value that
	// sorts first.
	want := Values{FirstName: "Muhammed Emin", LastName: "Yılmaz", City: "İstanbul"}
	if !maps.Equal(got, want) {
		t.Errorf("Longest = %v, want %v", got, want)
	}
	if tie := Longest([]Values{{City: "Bursa"}, {City: "Adana"}}); tie[City] != "Adana" {
		t.Errorf("tie = %q, want Adana", tie[City])
	}
	if empty := Longest(nil); empty[FirstName] != "" || len(empty) != len(Variables) {
		t.Errorf("Longest(nil) = %v", empty)
	}
}

func TestEmailHTML(t *testing.T) {
	got := EmailHTML("Sepet & indirim", "Merhaba Ayşe,\r\n\r\nSepetindeki <script>alert(1)</script> ürünler\nhâlâ seni bekliyor.\n\n\n%30 indirim!")
	for _, want := range []string{
		"<title>Sepet &amp; indirim</title>",
		"<p>Merhaba Ayşe,</p>",
		"<p>Sepetindeki &lt;script&gt;alert(1)&lt;/script&gt; ürünler<br>hâlâ seni bekliyor.</p>",
		"<p>%30 indirim!</p>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script>") || strings.Count(got, "<p>") != 3 {
		t.Errorf("unexpected HTML:\n%s", got)
	}
}
