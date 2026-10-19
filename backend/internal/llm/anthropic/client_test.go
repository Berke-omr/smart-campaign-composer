package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// fakeAPI serves a fixed response body and records the requests it got.
type fakeAPI struct {
	mu       sync.Mutex
	requests []capturedRequest
}

type capturedRequest struct {
	header http.Header
	body   []byte
}

func (f *fakeAPI) start(t *testing.T, status int, contentType string, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, capturedRequest{header: r.Header.Clone(), body: b})
		f.mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func testClient(baseURL string) *Client {
	return New(Config{APIKey: "test-key", Model: "claude-sonnet-5-5", RequestTimeout: 5 * time.Second, MaxRetries: 0, BaseURL: baseURL})
}

var testCities = []string{"İstanbul", "Ankara", "İzmir"}

func emailRequest() llm.Request {
	return llm.Request{
		Task: llm.TaskEmail, Brief: "b", Attempt: 1,
		System: llm.ContentSystem(content.Email), Prompt: "Write the email.",
		Schema: llm.ContentSchema(content.Email),
	}
}

const okMessage = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5",
 "content":[{"type":"thinking","thinking":"","signature":"sig"},{"type":"text","text":"{\"subject\":\"S\",\"body\":\"B\"}"}],
 "stop_reason":"end_turn","stop_sequence":null,
 "usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":0}}`

func TestRequestShape(t *testing.T) {
	api := &fakeAPI{}
	url := api.start(t, 200, "application/json", []byte(okMessage))
	if _, err := testClient(url).Complete(context.Background(), emailRequest(), nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	req := api.requests[0]
	if got := req.header.Get("X-Api-Key"); got != "test-key" {
		t.Errorf("X-Api-Key = %q, want the configured key", got)
	}
	var body struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    []struct {
			Text         string          `json:"text"`
			CacheControl json.RawMessage `json:"cache_control"`
		} `json:"system"`
		Thinking     map[string]any `json:"thinking"`
		OutputConfig struct {
			Effort string `json:"effort"`
			Format struct {
				Type   string          `json:"type"`
				Schema json.RawMessage `json:"schema"`
			} `json:"format"`
		} `json:"output_config"`
		ToolChoice json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("request body: %v\n%s", err, req.body)
	}
	if body.Model != "claude-sonnet-5-5" || body.MaxTokens != 8000 {
		t.Errorf("model/max_tokens = %s/%d", body.Model, body.MaxTokens)
	}
	if len(body.System) != 1 || body.System[0].Text != llm.ContentSystem(content.Email) || len(body.System[0].CacheControl) == 0 {
		t.Errorf("system block = %+v, want the system prompt with cache_control", body.System)
	}
	if body.Thinking["type"] != "adaptive" {
		t.Errorf("thinking = %v, want adaptive (disabled is a 400 on Sonnet 5.5)", body.Thinking)
	}
	if body.OutputConfig.Effort != "low" {
		t.Errorf("effort = %q, want it set explicitly", body.OutputConfig.Effort)
	}
	if body.OutputConfig.Format.Type != "json_schema" {
		t.Errorf("format type = %q", body.OutputConfig.Format.Type)
	}
	if len(body.ToolChoice) != 0 {
		t.Errorf("tool_choice must not be sent (forced tool use is a 400 on Sonnet 5.5): %s", body.ToolChoice)
	}
	// The schema must survive the SDK's encoding unchanged in meaning, and
	// the email's subject must still come before its body.
	if !jsonEqual(body.OutputConfig.Format.Schema, llm.ContentSchema(content.Email)) {
		t.Errorf("schema sent = %s\nwant %s", body.OutputConfig.Format.Schema, llm.ContentSchema(content.Email))
	}
	s := string(body.OutputConfig.Format.Schema)
	if strings.Index(s, `"subject"`) > strings.Index(s, `"body"`) {
		t.Errorf("property order lost: %s", s)
	}
}

func TestSegmentSchemaIsSentWhole(t *testing.T) {
	api := &fakeAPI{}
	url := api.start(t, 200, "application/json", []byte(okMessage))
	reg := segment.NewRegistry(testCities)
	req := llm.Request{Task: llm.TaskSegment, System: llm.SegmentSystem(reg), Prompt: "p", Schema: llm.SegmentSchema(reg)}
	_, _ = testClient(url).Complete(context.Background(), req, nil)

	var body struct {
		OutputConfig struct {
			Format struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"format"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(api.requests[0].body, &body); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(body.OutputConfig.Format.Schema, llm.SegmentSchema(reg)) {
		t.Error("segment schema changed on the way to the API")
	}
}

func TestStopReasonsAreHandledBeforeParsing(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(t *testing.T, err error)
	}{
		{
			name: "refusal",
			body: `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],
				"stop_reason":"refusal","stop_details":{"type":"refusal","category":"general_harms","explanation":"declined"},
				"usage":{"input_tokens":1,"output_tokens":0}}`,
			check: func(t *testing.T, err error) {
				var r *llm.RefusalError
				if !errors.As(err, &r) || r.Category != "general_harms" || r.Explanation != "declined" {
					t.Errorf("err = %v, want RefusalError(general_harms)", err)
				}
			},
		},
		{
			name: "refusal after partial text",
			body: `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5",
				"content":[{"type":"text","text":"{\"subject\":\"Hal"}],
				"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":""},
				"usage":{"input_tokens":1,"output_tokens":3}}`,
			check: func(t *testing.T, err error) {
				var r *llm.RefusalError
				if !errors.As(err, &r) {
					t.Errorf("err = %v, want RefusalError even though text was produced", err)
				}
			},
		},
		{
			name: "max_tokens",
			body: `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5",
				"content":[{"type":"text","text":"{\"subject\":\"S\",\"bo"}],
				"stop_reason":"max_tokens","usage":{"input_tokens":1,"output_tokens":8000}}`,
			check: func(t *testing.T, err error) {
				if !errors.Is(err, llm.ErrTruncated) {
					t.Errorf("err = %v, want ErrTruncated", err)
				}
			},
		},
		{
			name: "unexpected stop reason",
			body: `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5",
				"content":[{"type":"text","text":"{}"}],"stop_reason":"pause_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "pause_turn") {
					t.Errorf("err = %v, want an unexpected stop reason error", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{}
			url := api.start(t, 200, "application/json", []byte(tt.body))
			resp, err := testClient(url).Complete(context.Background(), emailRequest(), nil)
			if err == nil {
				t.Fatalf("got answer %q, want an error", resp.Text)
			}
			tt.check(t, err)
		})
	}
}

func TestAPIErrorsAreWrapped(t *testing.T) {
	api := &fakeAPI{}
	url := api.start(t, 400, "application/json",
		[]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"output_config.format.schema: too complex"}}`))
	_, err := testClient(url).Complete(context.Background(), emailRequest(), nil)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err = %v, want the API's 400", err)
	}
}

func TestTextBlocksOnlyAndUsage(t *testing.T) {
	api := &fakeAPI{}
	url := api.start(t, 200, "application/json", []byte(okMessage))
	resp, err := testClient(url).Complete(context.Background(), emailRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"subject":"S","body":"B"}` {
		t.Errorf("text = %q (thinking blocks must be skipped)", resp.Text)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 || resp.Usage.CacheReadTokens != 3 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

// recorded reads a response captured by the live smoke test (live_test.go).
func recorded(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "recorded", name))
	if err != nil {
		t.Fatalf("recorded fixture: %v", err)
	}
	return b
}

// Replay tests: responses recorded from the real API (live_test.go) are
// served back to the real SDK, so the parsing path is tested against the
// actual response shape without an API key.

func replay(t *testing.T, fixture string) string {
	t.Helper()
	contentType := "application/json"
	if strings.HasSuffix(fixture, ".sse") {
		contentType = "text/event-stream"
	}
	api := &fakeAPI{}
	return api.start(t, 200, contentType, recorded(t, fixture))
}

func TestReplaySegmentAnswers(t *testing.T) {
	reg := segment.NewRegistry([]string{
		"İstanbul", "Ankara", "İzmir", "Bursa", "Antalya", "Adana", "Konya", "Gaziantep",
		"Kocaeli", "Eskişehir", "Mersin", "Kayseri", "Samsun", "Trabzon", "Diyarbakır",
	})
	tests := []struct {
		fixture         string
		wantConditions  int
		wantUnsupported int
	}{
		{"segment_sample_low.json", 3, 0},
		{"segment_any_city_low.json", 2, 0},
		{"segment_unsupported_low.json", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			req := llm.Request{Task: llm.TaskSegment, System: "s", Prompt: "p", Schema: llm.SegmentSchema(reg)}
			resp, err := testClient(replay(t, tt.fixture)).Complete(context.Background(), req, nil)
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			p, err := llm.ParseSegment(resp.Text)
			if err != nil {
				t.Fatalf("ParseSegment: %v", err)
			}
			v, err := segment.ParseAndValidate(p.Rule, reg)
			if err != nil {
				t.Fatalf("recorded rule rejected by the validator: %v", err)
			}
			if v.Len() != tt.wantConditions || len(p.UnsupportedCriteria) != tt.wantUnsupported {
				t.Errorf("conditions = %d, unsupported = %q", v.Len(), p.UnsupportedCriteria)
			}
			if resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens == 0 {
				t.Errorf("usage not read: %+v", resp.Usage)
			}
		})
	}
}

func TestReplayRecordedRefusal(t *testing.T) {
	// A real refusal: the model wrote part of the JSON, then the request was
	// declined. The partial text must not be parsed.
	req := llm.Request{Task: llm.TaskSegment, System: "s", Prompt: "p", Schema: llm.ContentSchema(content.SMS)}
	_, err := testClient(replay(t, "segment_sample_medium.json")).Complete(context.Background(), req, nil)
	var r *llm.RefusalError
	if !errors.As(err, &r) || r.Category != "reasoning_extraction" {
		t.Fatalf("err = %v, want RefusalError(reasoning_extraction)", err)
	}
}

func TestReplayStreamedDrafts(t *testing.T) {
	for _, tt := range []struct {
		fixture string
		ch      content.Channel
		task    llm.Task
	}{
		{"sms_sample_low.sse", content.SMS, llm.TaskSMS},
		{"email_sample_low.sse", content.Email, llm.TaskEmail},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			req := llm.Request{Task: tt.task, System: "s", Prompt: "p", Schema: llm.ContentSchema(tt.ch)}
			var fragments []string
			resp, err := testClient(replay(t, tt.fixture)).Complete(context.Background(), req, func(s string) {
				fragments = append(fragments, s)
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(fragments) < 2 {
				t.Errorf("got %d fragments; the answer should arrive in pieces", len(fragments))
			}
			if strings.Join(fragments, "") != resp.Text {
				t.Error("streamed fragments do not add up to the final text")
			}
			m, err := llm.ParseDraft(tt.ch, resp.Text)
			if err != nil {
				t.Fatalf("ParseDraft: %v", err)
			}
			if tt.ch == content.Email {
				if m.Subject == "" || m.Body == "" {
					t.Errorf("email = %+v", m)
				}
				// The schema's property order reached the model: the subject streams first.
				if !strings.HasPrefix(strings.Join(fragments, ""), `{"subject"`) {
					t.Errorf("stream starts with %.30q, want the subject first", strings.Join(fragments, ""))
				}
			} else if m.Text == "" {
				t.Error("empty SMS")
			}
		})
	}
}

// The recorded real answers also pass the whole generator: validation,
// states and streamed deltas, not just the client.
func TestReplayThroughTheGenerator(t *testing.T) {
	reg := segment.NewRegistry([]string{"İstanbul", "Ankara", "İzmir"})
	gen := &llm.Generator{Client: testClient(replay(t, "segment_sample_low.json")), AttemptTimeout: 5 * time.Second}
	res, err := gen.Segment(context.Background(), reg, "brief")
	if err != nil || res.Attempts != 1 || res.Validated.Len() != 3 || res.CampaignName != "Cart reminder — İstanbul" {
		t.Fatalf("Segment = %+v, %v", res, err)
	}

	gen.Client = testClient(replay(t, "sms_sample_low.sse"))
	var states []string
	deltas := 0
	results := gen.Drafts(context.Background(), llm.DraftInput{
		Brief: "brief", Tone: llm.ToneFriendly, CampaignName: res.CampaignName,
		Channels: []content.Channel{content.SMS},
		Longest: map[content.Channel]content.Values{content.SMS: {
			content.FirstName: "Muhammed Emin", content.LastName: "Montgomery-Fitzgerald", content.City: "İstanbul"}},
	}, func(e llm.Event) {
		if e.Type == llm.EventState {
			states = append(states, string(e.State))
		} else {
			deltas++
		}
	})
	if r := results[content.SMS]; r.State != llm.StateValid || r.Report.Lengths[0].WorstCaseLength != 116 {
		t.Errorf("SMS = %+v", r)
	}
	if strings.Join(states, " ") != "writing validating valid" || deltas < 2 {
		t.Errorf("states %v, %d deltas", states, deltas)
	}
}
