package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

var update = flag.Bool("update", false, "rewrite the golden prompt files")

// The seed's cities, most common first: the order the api reads them in.
var testCities = []string{
	"İstanbul", "Ankara", "İzmir", "Bursa", "Antalya", "Adana", "Konya", "Gaziantep",
	"Kocaeli", "Eskişehir", "Mersin", "Kayseri", "Samsun", "Trabzon", "Diyarbakır",
}

func testRegistry() *segment.Registry { return segment.NewRegistry(testCities) }

func compile(t *testing.T, raw json.RawMessage) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("s.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("s.json")
	if err != nil {
		t.Fatalf("not a valid JSON Schema: %v", err)
	}
	return sch
}

func conforms(t *testing.T, sch *jsonschema.Schema, answer string) bool {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(answer))
	if err != nil {
		t.Fatalf("bad test JSON %s: %v", answer, err)
	}
	return sch.Validate(inst) == nil
}

// Keywords outside the Structured Outputs subset we rely on.
var unsupportedKeywords = []string{
	"$ref", "$defs", "definitions", "minLength", "maxLength", "pattern",
	"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
	"minItems", "maxItems", "uniqueItems",
}

func checkSubset(t *testing.T, name string, raw json.RawMessage) {
	t.Helper()
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch n := node.(type) {
		case map[string]any:
			for _, k := range unsupportedKeywords {
				if _, ok := n[k]; ok {
					t.Errorf("%s%s uses unsupported keyword %q", name, path, k)
				}
			}
			if n["type"] == "object" {
				if n["additionalProperties"] != false {
					t.Errorf("%s%s: object without additionalProperties:false", name, path)
				}
				props, _ := n["properties"].(map[string]any)
				required, _ := n["required"].([]any)
				if len(required) != len(props) {
					t.Errorf("%s%s: %d properties but %d required", name, path, len(props), len(required))
				}
			}
			for k, v := range n {
				walk(path+"."+k, v)
			}
		case []any:
			for _, v := range n {
				walk(path+"[]", v)
			}
		}
	}
	walk("$", root)
}

func TestSchemasFitStructuredOutputsSubset(t *testing.T) {
	checkSubset(t, "segment", SegmentSchema(testRegistry()))
	checkSubset(t, "sms", ContentSchema(content.SMS))
	checkSubset(t, "email", ContentSchema(content.Email))
}

func TestSchemasAreDeterministic(t *testing.T) {
	// The schema is part of every request; if its bytes changed between
	// calls, the API would recompile it and the prompt cache would miss.
	if !bytes.Equal(SegmentSchema(testRegistry()), SegmentSchema(testRegistry())) {
		t.Fatal("segment schema serialization differs between calls")
	}
}

func TestEmailSchemaPutsSubjectBeforeBody(t *testing.T) {
	// The model generates properties in schema order; the UI streams the
	// subject first.
	s := string(ContentSchema(content.Email))
	if strings.Index(s, `"subject"`) > strings.Index(s, `"body"`) {
		t.Fatalf("subject must come before body: %s", s)
	}
}

func TestSegmentSchemaAcceptsAndRejects(t *testing.T) {
	sch := compile(t, SegmentSchema(testRegistry()))
	sample := `{"campaign_name":"Cart reminder — İstanbul","rule":{"match":"all","conditions":[
		{"field":"city","op":"eq","value":"İstanbul"},
		{"field":"last_cart_add_at","op":"within_last_days","value":30},
		{"field":"last_purchase_at","op":"not_within_last_days","value":30}]},
		"unsupported_criteria":[]}`
	if !conforms(t, sch, sample) {
		t.Error("the sample brief's answer is rejected")
	}
	for name, bad := range map[string]string{
		"missing name":      `{"rule":{"match":"all","conditions":[]},"unsupported_criteria":[]}`,
		"extra key":         `{"campaign_name":"x","rule":{"match":"all","conditions":[]},"unsupported_criteria":[],"sql":"DROP TABLE users"}`,
		"unknown city":      `{"campaign_name":"x","rule":{"match":"all","conditions":[{"field":"city","op":"eq","value":"Paris"}]},"unsupported_criteria":[]}`,
		"criteria not text": `{"campaign_name":"x","rule":{"match":"all","conditions":[]},"unsupported_criteria":[1]}`,
	} {
		if conforms(t, sch, bad) {
			t.Errorf("%s: accepted %s", name, bad)
		}
	}
}

func TestContentSchemasAcceptAndReject(t *testing.T) {
	sms := compile(t, ContentSchema(content.SMS))
	email := compile(t, ContentSchema(content.Email))
	if !conforms(t, sms, `{"text":"Merhaba {{first_name}}"}`) {
		t.Error("valid SMS rejected")
	}
	if !conforms(t, email, `{"subject":"Merhaba","body":"Merhaba {{first_name}},\n\nSepetin seni bekliyor."}`) {
		t.Error("valid email rejected")
	}
	if conforms(t, sms, `{"text":"x","html":"<b>x</b>"}`) || conforms(t, email, `{"subject":"x"}`) {
		t.Error("invalid content accepted")
	}
}

func TestParseSegment(t *testing.T) {
	p, err := ParseSegment(`{"campaign_name":"  Cart reminder — İstanbul ","rule":{"match":"all","conditions":[]},"unsupported_criteria":["kadın kullanıcılar"," "]}`)
	if err != nil {
		t.Fatalf("ParseSegment: %v", err)
	}
	if p.CampaignName != "Cart reminder — İstanbul" {
		t.Errorf("name = %q (want trimmed)", p.CampaignName)
	}
	if len(p.UnsupportedCriteria) != 1 || p.UnsupportedCriteria[0] != "kadın kullanıcılar" {
		t.Errorf("unsupported = %q (blank entries are dropped)", p.UnsupportedCriteria)
	}
	if string(p.Rule) != `{"match":"all","conditions":[]}` {
		t.Errorf("rule = %s", p.Rule)
	}

	none, err := ParseSegment(`{"campaign_name":"x","rule":{"match":"all","conditions":[]},"unsupported_criteria":[]}`)
	if err != nil || none.UnsupportedCriteria == nil {
		t.Errorf("empty criteria must decode to an empty list, got %v, %v", none.UnsupportedCriteria, err)
	}
}

func TestParseSegmentRejects(t *testing.T) {
	long := strings.Repeat("ş", MaxCampaignNameRunes+1)
	tests := []struct {
		name, answer, want string
	}{
		{"not JSON", `Here is the rule: {...}`, "not the expected JSON object"},
		{"truncated JSON", `{"campaign_name":"x","rule":{"match":`, "not the expected JSON object"},
		{"unknown key", `{"campaign_name":"x","rule":{},"unsupported_criteria":[],"sql":"x"}`, `unknown field "sql"`},
		{"trailing data", `{"campaign_name":"x","rule":{},"unsupported_criteria":[]} {}`, "extra data"},
		{"missing name", `{"rule":{},"unsupported_criteria":[]}`, "campaign_name is missing"},
		{"null rule", `{"campaign_name":"x","rule":null,"unsupported_criteria":[]}`, "rule is missing"},
		{"missing criteria", `{"campaign_name":"x","rule":{}}`, "unsupported_criteria is missing"},
		{"blank name", `{"campaign_name":"  ","rule":{},"unsupported_criteria":[]}`, "campaign_name is empty"},
		{"long name (runes, not bytes)", `{"campaign_name":"` + long + `","rule":{},"unsupported_criteria":[]}`, "61 characters; the limit is 60"},
		{"two-line name", `{"campaign_name":"a\nb","rule":{},"unsupported_criteria":[]}`, "single line"},
		{"too many criteria", `{"campaign_name":"x","rule":{},"unsupported_criteria":["1","2","3","4","5","6","7","8","9","10","11"]}`, "11 entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSegment(tt.answer)
			var probs Problems
			if err == nil || !errors.As(err, &probs) {
				t.Fatalf("err = %v, want Problems", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}

	// A name of exactly 60 Turkish characters is fine.
	ok := strings.Repeat("ş", MaxCampaignNameRunes)
	if _, err := ParseSegment(`{"campaign_name":"` + ok + `","rule":{},"unsupported_criteria":[]}`); err != nil {
		t.Errorf("60-character name rejected: %v", err)
	}
}

func TestParseDraft(t *testing.T) {
	m, err := ParseDraft(content.SMS, `{"text":"Merhaba {{first_name}}!"}`)
	if err != nil || m.Text != "Merhaba {{first_name}}!" {
		t.Errorf("sms = %+v, %v", m, err)
	}
	m, err = ParseDraft(content.Email, `{"subject":"Konu","body":"Gövde"}`)
	if err != nil || m.Subject != "Konu" || m.Body != "Gövde" {
		t.Errorf("email = %+v, %v", m, err)
	}

	for name, tt := range map[string]struct {
		ch           content.Channel
		answer, want string
	}{
		"sms answer for email": {content.Email, `{"text":"x"}`, `unknown field "text"`},
		"missing subject":      {content.Email, `{"body":"x"}`, "subject is missing"},
		"missing body":         {content.Email, `{"subject":"x"}`, "body is missing"},
		"missing text":         {content.SMS, `{}`, "text is missing"},
		"html key":             {content.SMS, `{"text":"x","html":"<b>"}`, `unknown field "html"`},
		"not an object":        {content.SMS, `"just text"`, "not the expected JSON object"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDraft(tt.ch, tt.answer)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestToneValid(t *testing.T) {
	for _, tone := range Tones {
		if !tone.Valid() {
			t.Errorf("%q not valid", tone)
		}
	}
	if Tone("sarcastic").Valid() || Tone("").Valid() {
		t.Error("unknown tone accepted")
	}
}

func TestPromptsAreStable(t *testing.T) {
	// System prompts are the cached prefix: they must not change between
	// requests (no timestamps, no request data).
	if SegmentSystem(testRegistry()) != SegmentSystem(testRegistry()) {
		t.Fatal("segment system prompt differs between calls")
	}
	brief := "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
	for _, sys := range []string{SegmentSystem(testRegistry()), ContentSystem(content.SMS), ContentSystem(content.Email)} {
		if strings.Contains(sys, "İstanbul'daki") || strings.Contains(sys, "<brief>") {
			t.Errorf("system prompt contains request data:\n%s", sys)
		}
	}
	if !strings.Contains(SegmentPrompt(brief, nil), brief) {
		t.Error("segment prompt does not carry the brief")
	}
}

// sampleBrief is the case's example; the content prompt below uses the
// longest names in the seed so the golden file shows real numbers.
const sampleBrief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."

func sampleContentBrief() ContentBrief {
	return ContentBrief{
		Brief:        sampleBrief,
		Tone:         ToneFriendly,
		CampaignName: "Cart reminder — İstanbul",
		Audience:     []string{"City is İstanbul", "Added to cart in the last 30 days", "No purchase in the last 30 days"},
		Longest:      content.Values{content.FirstName: "Muhammed Emin", content.LastName: "Hacıosmanoğlu", content.City: "İstanbul"},
	}
}

// Golden files make every prompt reviewable in a diff. Regenerate with
// go test ./internal/llm -run TestPromptGolden -update
func TestPromptGolden(t *testing.T) {
	repair := &Repair{
		Previous: `{"text":"Merhaba {{first_name}}, sepetindeki ürünler seni bekliyor! ..."}`,
		Problems: []string{"text is 172 characters with the longest values filled in; the limit is 160. Remove at least 12 characters."},
	}
	files := map[string]string{
		"segment_system.txt":  SegmentSystem(testRegistry()),
		"segment_user.txt":    SegmentPrompt(sampleBrief, nil),
		"sms_system.txt":      ContentSystem(content.SMS),
		"email_system.txt":    ContentSystem(content.Email),
		"sms_user.txt":        ContentPrompt(content.SMS, sampleContentBrief(), nil),
		"sms_user_repair.txt": ContentPrompt(content.SMS, sampleContentBrief(), repair),
		"email_user.txt":      ContentPrompt(content.Email, sampleContentBrief(), nil),
	}
	for name, got := range files {
		path := filepath.Join("testdata", "prompts", name)
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update to create it)", err)
		}
		if got != string(want) {
			t.Errorf("%s changed; review the diff and run with -update:\n--- got ---\n%s", name, got)
		}
	}
}
