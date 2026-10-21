package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/campaign"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

const sampleBrief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."

// campaignServer runs the api against a real database and the mock LLM.
func campaignServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRouter(campaignDeps(t, mock.Options{ChunkRunes: 8})))
	t.Cleanup(srv.Close)
	return srv
}

// campaignDeps wires the api to a real database and the mock LLM, which
// streams as opts say (its cities are filled in).
func campaignDeps(t *testing.T, opts mock.Options) Deps {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, seed.Generate(5000, 42, fixedNow), false); err != nil {
		t.Fatal(err)
	}
	cities, err := users.NewRepository(pool).DistinctCities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := mock.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	reg := segment.NewRegistry(cities)
	counter := segment.NewCounter(pool)
	opts.Cities = cities
	svc := &campaign.Service{
		Repo:      campaign.NewRepository(pool),
		Generator: &llm.Generator{Client: mock.New(cat, opts), AttemptTimeout: time.Second},
		Registry:  reg, Audience: counter, LLMMode: "mock", Logger: logging.Discard(),
		Now: func() time.Time { return fixedNow },
	}
	return Deps{
		Logger: logging.Discard(), Segments: reg, Audience: counter, Campaigns: svc,
		Now: func() time.Time { return fixedNow },
	}
}

func do(t *testing.T, method, url, body string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

type event struct {
	name string
	data map[string]any
}

// readSSE parses a text/event-stream body.
func readSSE(t *testing.T, r io.Reader) []event {
	t.Helper()
	var out []event
	var cur event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("event data is not JSON: %v", err)
			}
		case line == "":
			out = append(out, cur)
			cur = event{}
		}
	}
	return out
}

func TestCampaignFlowOverHTTP(t *testing.T) {
	srv := campaignServer(t)

	resp, body := do(t, http.MethodPost, srv.URL+"/api/campaigns",
		fmt.Sprintf(`{"brief":%q,"channels":["sms","email"],"tone":"friendly"}`, sampleBrief))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var created struct {
		ID       int64          `json:"id"`
		Name     string         `json:"name"`
		Status   string         `json:"status"`
		Audience map[string]any `json:"audience"`
		Messages map[string]any `json:"messages"`
		LLM      map[string]any `json:"llm"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Cart reminder — İstanbul" || created.Status != "draft" || created.Audience["channel_messages"] != 232.0 ||
		created.Messages["sms"] != nil || created.LLM["mode"] != "mock" {
		t.Errorf("created = %s", body)
	}

	stream, err := http.Get(fmt.Sprintf("%s/api/campaigns/%d/draft/stream", srv.URL, created.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if ct := stream.Header.Get("Content-Type"); ct != "text/event-stream" || stream.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers: Content-Type %q, X-Accel-Buffering %q", ct, stream.Header.Get("X-Accel-Buffering"))
	}
	events := readSSE(t, stream.Body)

	states := map[string][]string{}
	deltas := 0
	for _, e := range events {
		switch e.name {
		case llm.EventState:
			ch := e.data["channel"].(string)
			states[ch] = append(states[ch], e.data["state"].(string))
		case llm.EventDelta:
			deltas++
		}
	}
	for _, ch := range []string{"sms", "email"} {
		if got := strings.Join(states[ch], " "); got != "writing validating valid" {
			t.Errorf("%s states = %s", ch, got)
		}
	}
	last := events[len(events)-1]
	if deltas < 10 || last.name != campaign.EventDone || last.data["status"] != "ready" {
		t.Errorf("%d deltas, last event %+v", deltas, last)
	}

	// An over-limit edit is rejected with the validation report.
	url := fmt.Sprintf("%s/api/campaigns/%d/messages/sms", srv.URL, created.ID)
	resp, body = do(t, http.MethodPut, url, fmt.Sprintf(`{"text":%q}`, strings.Repeat("ş", 150)+"{{first_name}}"))
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"too_long"`) || !strings.Contains(string(body), `"worst_case_length":163`) {
		t.Errorf("over-limit edit: %d %s", resp.StatusCode, body)
	}
	// A valid edit is stored.
	resp, body = do(t, http.MethodPut, url, `{"text":"Merhaba {{first_name}}, sepetin seni bekliyor!"}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"source":"edited"`) || !strings.Contains(string(body), `"status":"ready"`) {
		t.Errorf("valid edit: %d %s", resp.StatusCode, body)
	}
	// Email fields on the SMS channel are refused.
	if resp, _ := do(t, http.MethodPut, url, `{"subject":"x","body":"y"}`); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("email fields on SMS: %d", resp.StatusCode)
	}

	resp, body = do(t, http.MethodGet, srv.URL+"/api/campaigns", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"Cart reminder — İstanbul"`) {
		t.Errorf("list: %d %s", resp.StatusCode, body)
	}
}

func TestCampaignErrorsOverHTTP(t *testing.T) {
	srv := campaignServer(t)
	tests := []struct {
		name, method, path, body string
		want                     int
		contains                 string
	}{
		{"empty brief", "POST", "/api/campaigns", `{"brief":" ","channels":["sms"]}`, 422, "brief is empty"},
		{"unknown key", "POST", "/api/campaigns", `{"brief":"x","channels":["sms"],"sql":"x"}`, 422, `unknown field`},
		{"unknown channel", "POST", "/api/campaigns", `{"brief":"x","channels":["fax"]}`, 422, "unknown channel"},
		{"unknown campaign", "GET", "/api/campaigns/999", "", 404, "campaign not found"},
		{"bad id", "GET", "/api/campaigns/abc", "", 404, "campaign not found"},
		{"stream unknown campaign", "GET", "/api/campaigns/999/draft/stream", "", 404, "campaign not found"},
		{"unknown message channel", "PUT", "/api/campaigns/1/messages/push", `{"text":"x"}`, 404, "unknown channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := do(t, tt.method, srv.URL+tt.path, tt.body)
			if resp.StatusCode != tt.want || !strings.Contains(string(body), tt.contains) {
				t.Errorf("%d %s, want %d containing %q", resp.StatusCode, body, tt.want, tt.contains)
			}
		})
	}
}

func TestApprovalOverHTTP(t *testing.T) {
	srv := campaignServer(t)
	create := func() int64 {
		resp, body := do(t, http.MethodPost, srv.URL+"/api/campaigns",
			fmt.Sprintf(`{"brief":%q,"channels":["sms","email"],"tone":"friendly"}`, sampleBrief))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create: %d %s", resp.StatusCode, body)
		}
		var c struct{ ID int64 }
		_ = json.Unmarshal(body, &c)
		return c.ID
	}

	draft := create()
	resp, body := do(t, http.MethodPost, fmt.Sprintf("%s/api/campaigns/%d/approve", srv.URL, draft), "")
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "not ready") {
		t.Errorf("approving a draft: %d %s", resp.StatusCode, body)
	}

	id := create()
	stream, err := http.Get(fmt.Sprintf("%s/api/campaigns/%d/draft/stream", srv.URL, id))
	if err != nil {
		t.Fatal(err)
	}
	readSSE(t, stream.Body)
	stream.Body.Close()

	url := fmt.Sprintf("%s/api/campaigns/%d/approve", srv.URL, id)
	resp, first := do(t, http.MethodPost, url, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(first), `"status":"sending"`) ||
		!strings.Contains(string(first), `"deliveries":{"sms":99,"email":133,"total":232}`) {
		t.Fatalf("approve: %d %s", resp.StatusCode, first)
	}
	resp, second := do(t, http.MethodPost, url, "")
	if resp.StatusCode != http.StatusOK || string(second) != string(first) {
		t.Errorf("approving again: %d %s, want the same 200 body %s", resp.StatusCode, second, first)
	}

	resp, body = do(t, http.MethodGet, fmt.Sprintf("%s/api/campaigns/%d", srv.URL, id), "")
	if !strings.Contains(string(body), `"status":"sending"`) || strings.Contains(string(body), `"approved_at":null`) {
		t.Errorf("campaign after approval: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, http.MethodPut, fmt.Sprintf("%s/api/campaigns/%d/messages/sms", srv.URL, id), `{"text":"Merhaba"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("edit after approval: %d %s", resp.StatusCode, body)
	}
	if resp, body := do(t, http.MethodPost, srv.URL+"/api/campaigns/999/approve", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown campaign: %d %s", resp.StatusCode, body)
	}
}
