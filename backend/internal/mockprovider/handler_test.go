package mockprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
)

// quiet is a provider that never misbehaves unless a test says so.
var quiet = config.MockProvider{}

func server(t *testing.T, cfg config.MockProvider, decide func(Request) Outcome) (*Provider, *httptest.Server) {
	t.Helper()
	p := New(cfg, 1)
	if decide != nil {
		p.Decide = decide
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return p, srv
}

type answer struct {
	status     int
	body       map[string]string
	replayed   bool
	retryAfter string
}

func post(t *testing.T, client *http.Client, url, channel, key, body string) (answer, error) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/"+channel, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer resp.Body.Close()
	a := answer{status: resp.StatusCode, replayed: resp.Header.Get("Idempotent-Replayed") == "true",
		retryAfter: resp.Header.Get("Retry-After")}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &a.body)
	return a, nil
}

const sms = `{"to":"+905321234567","text":"Merhaba Ayşe"}`

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	New(quiet, 1).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestRepeatedKeyGetsTheOriginalAnswer is D5 layer 2: a retry with the same
// key does not make a second message.
func TestRepeatedKeyGetsTheOriginalAnswer(t *testing.T) {
	p, srv := server(t, quiet, nil)
	first, err := post(t, srv.Client(), srv.URL, "sms", "k1", sms)
	if err != nil || first.status != http.StatusAccepted || first.body["id"] == "" || first.replayed {
		t.Fatalf("first = %+v (err %v)", first, err)
	}
	again, err := post(t, srv.Client(), srv.URL, "sms", "k1", sms)
	if err != nil || again.status != http.StatusAccepted || again.body["id"] != first.body["id"] || !again.replayed {
		t.Fatalf("again = %+v (err %v), want the first answer replayed", again, err)
	}
	other, _ := post(t, srv.Client(), srv.URL, "sms", "k2", sms)
	if other.body["id"] == first.body["id"] {
		t.Errorf("another key got the same id %s", other.body["id"])
	}
	if n := len(p.Messages()); n != 2 {
		t.Errorf("%d messages recorded, want 2", n)
	}
	if s := p.Stats()["sms"]; s.Received != 3 || s.Accepted != 2 || s.Replayed != 1 {
		t.Errorf("stats = %+v", s)
	}
}

func TestConcurrentRequestsWithOneKeyRecordOneMessage(t *testing.T) {
	p, srv := server(t, config.MockProvider{LatencyMax: 5 * time.Millisecond}, nil)
	const n = 50
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := post(t, srv.Client(), srv.URL, "sms", "same", sms)
			if err != nil || a.status != http.StatusAccepted {
				t.Errorf("request %d = %+v (err %v)", i, a, err)
			}
			ids[i] = a.body["id"]
		}()
	}
	wg.Wait()
	for i := range ids {
		if ids[i] != ids[0] {
			t.Fatalf("request %d got id %s, request 0 got %s", i, ids[i], ids[0])
		}
	}
	if m := p.Messages(); len(m) != 1 {
		t.Errorf("%d messages recorded for one key, want 1", len(m))
	}
}

// TestFailuresAreNotStored: after a 503 or a 429 the provider has not taken
// the message, so a retry with the same key is processed again.
func TestFailuresAreNotStored(t *testing.T) {
	script := map[int]Outcome{1: ServerError, 2: Throttle, 3: Accept}
	p, srv := server(t, quiet, func(r Request) Outcome { return script[r.Call] })

	a, _ := post(t, srv.Client(), srv.URL, "sms", "k", sms)
	if a.status != http.StatusServiceUnavailable {
		t.Errorf("call 1 = %d, want 503", a.status)
	}
	a, _ = post(t, srv.Client(), srv.URL, "sms", "k", sms)
	if ra, err := strconv.Atoi(a.retryAfter); a.status != http.StatusTooManyRequests || err != nil || ra < 1 || ra > 2 {
		t.Errorf("call 2 = %d, Retry-After %q; want 429 with 1 or 2", a.status, a.retryAfter)
	}
	a, _ = post(t, srv.Client(), srv.URL, "sms", "k", sms)
	if a.status != http.StatusAccepted || a.replayed {
		t.Errorf("call 3 = %+v, want a fresh 202", a)
	}
	if s := p.Stats()["sms"]; s.Errors != 1 || s.Throttled != 1 || s.Accepted != 1 || len(p.Messages()) != 1 {
		t.Errorf("stats = %+v, %d messages", s, len(p.Messages()))
	}
}

// TestRejectionIsFinalAndFollowsTheRecipient: a rejected recipient is
// rejected again on a retry and under any other key.
func TestRejectionIsFinalAndFollowsTheRecipient(t *testing.T) {
	p, srv := server(t, config.MockProvider{RejectRate: 0.5}, nil)
	var rejectedTo, acceptedTo string
	for i := 0; rejectedTo == "" || acceptedTo == ""; i++ {
		to := fmt.Sprintf("+90532%07d", i)
		if rejected(to, 0.5) {
			rejectedTo = to
		} else {
			acceptedTo = to
		}
	}
	for i, key := range []string{"a", "a", "b"} {
		a, _ := post(t, srv.Client(), srv.URL, "sms", key, `{"to":"`+rejectedTo+`","text":"x"}`)
		if a.status != http.StatusBadRequest || a.body["error"] != "invalid recipient" || a.replayed != (i == 1) {
			t.Errorf("request %d (key %s) = %+v, want 400 invalid recipient", i, key, a)
		}
	}
	if a, _ := post(t, srv.Client(), srv.URL, "sms", "c", `{"to":"`+acceptedTo+`","text":"x"}`); a.status != http.StatusAccepted {
		t.Errorf("other recipient = %+v, want 202", a)
	}
	if s := p.Stats()["sms"]; s.Rejected != 2 || s.Accepted != 1 {
		t.Errorf("stats = %+v", s)
	}
}

// TestSlowAcceptRecordsOnce is the "accepted but the answer was lost" case:
// the client times out, retries with the same key and gets the original
// answer at once. One message exists.
func TestSlowAcceptRecordsOnce(t *testing.T) {
	p, srv := server(t, config.MockProvider{SlowDelay: 2 * time.Second},
		func(r Request) Outcome {
			if r.Call == 1 {
				return SlowAccept
			}
			return Accept
		})
	impatient := &http.Client{Timeout: 100 * time.Millisecond}
	if _, err := post(t, impatient, srv.URL, "sms", "k", sms); err == nil || !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("first call: err = %v, want a client timeout", err)
	}
	start := time.Now()
	a, err := post(t, impatient, srv.URL, "sms", "k", sms)
	if err != nil || a.status != http.StatusAccepted || !a.replayed {
		t.Fatalf("retry = %+v (err %v), want the stored 202", a, err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("retry took %s; a replay should not wait", d)
	}
	if m := p.Messages(); len(m) != 1 || p.Stats()["sms"].Slow != 1 {
		t.Errorf("%d messages, stats %+v; want 1 message", len(m), p.Stats()["sms"])
	}
}

// TestRateLimitPerChannel: the limit counts the requests of the last second
// per channel; over it the answer is 429 with the seconds until a slot frees.
func TestRateLimitPerChannel(t *testing.T) {
	p, srv := server(t, config.MockProvider{RateLimit: 5}, nil)
	for i := range 5 {
		if a, _ := post(t, srv.Client(), srv.URL, "sms", fmt.Sprint("s", i), sms); a.status != http.StatusAccepted {
			t.Fatalf("request %d = %d, want 202", i, a.status)
		}
	}
	a, _ := post(t, srv.Client(), srv.URL, "sms", "s5", sms)
	if a.status != http.StatusTooManyRequests || a.retryAfter != "1" || a.body["error"] != "rate limit exceeded" {
		t.Errorf("6th request = %+v, want 429 with Retry-After 1", a)
	}
	// Email has its own window.
	if a, _ := post(t, srv.Client(), srv.URL, "email", "e0", `{"to":"a@b.c","subject":"s","text":"t","html":"<p>t</p>"}`); a.status != http.StatusAccepted {
		t.Errorf("email = %d, want 202", a.status)
	}
	time.Sleep(1100 * time.Millisecond)
	if a, _ := post(t, srv.Client(), srv.URL, "sms", "s5", sms); a.status != http.StatusAccepted {
		t.Errorf("after the window = %d, want 202", a.status)
	}
	s := p.Stats()["sms"]
	if s.Limited != 1 || s.PeakPerSecond != 6 || s.Accepted != 6 {
		t.Errorf("stats = %+v, want 1 limited, a peak of 6 requests in a second, 6 accepted", s)
	}
}

func TestBadRequests(t *testing.T) {
	_, srv := server(t, quiet, nil)
	tests := []struct {
		name, channel, key, body string
		want                     int
		contains                 string
	}{
		{"no key", "sms", "", sms, 400, "Idempotency-Key"},
		{"unknown channel", "push", "k", sms, 404, "unknown channel"},
		{"not JSON", "sms", "k", `{`, 400, "invalid JSON"},
		{"unknown field", "sms", "k", `{"to":"x","text":"y","cc":"z"}`, 400, "invalid JSON"},
		{"no recipient", "sms", "k", `{"text":"y"}`, 400, `"to" is required`},
		{"email without subject", "email", "k", `{"to":"a@b.c","text":"y"}`, 400, `"subject" is required`},
		{"SMS with a subject", "sms", "k", `{"to":"x","text":"y","subject":"z"}`, 400, "only text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := post(t, srv.Client(), srv.URL, tt.channel, tt.key, tt.body)
			if a.status != tt.want || !strings.Contains(a.body["error"], tt.contains) {
				t.Errorf("= %+v, want %d containing %q", a, tt.want, tt.contains)
			}
		})
	}
}

// TestRandomOutcomesFollowTheRates checks the default Decide over many draws.
func TestRandomOutcomesFollowTheRates(t *testing.T) {
	p := New(config.MockProvider{ErrorRate: 0.03, ThrottleRate: 0.03, SlowRate: 0.005, RejectRate: 0.01}, 42)
	const n = 200_000
	counts := map[Outcome]int{}
	for i := range n {
		counts[p.Decide(Request{Channel: "sms", To: fmt.Sprintf("user%d@example.com", i)})]++
	}
	for outcome, want := range map[Outcome]float64{ServerError: 0.03 * 0.99, Throttle: 0.03 * 0.99, SlowAccept: 0.005 * 0.99, Reject: 0.01} {
		got := float64(counts[outcome]) / n
		if got < want*0.9 || got > want*1.1 {
			t.Errorf("outcome %d: %.4f of requests, want %.4f ± 10%%", outcome, got, want)
		}
	}
}
