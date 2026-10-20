package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/mockprovider"
)

func client(url string, timeout time.Duration) *Client {
	return &Client{BaseURL: url, HTTP: &http.Client{Timeout: timeout}}
}

var smsMsg = Message{Key: "8f6c2d1e-0000-4000-8000-000000000001", Channel: content.SMS, To: "+905321234567", Text: "Merhaba Ayşe"}

// TestClientSpeaksTheMockProvidersAPI: the client's requests are accepted by
// the real mock provider handler, carry the key, and a retry with the same
// key gets the same message id.
func TestClientSpeaksTheMockProvidersAPI(t *testing.T) {
	p := mockprovider.New(config.MockProvider{}, 1)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	c := client(srv.URL, time.Second)

	r1, err := c.Send(context.Background(), smsMsg)
	if err != nil || r1.Kind != Sent || r1.Status != 202 || r1.MessageID == "" {
		t.Fatalf("SMS = %+v (err %v)", r1, err)
	}
	r2, _ := c.Send(context.Background(), smsMsg)
	if r2.MessageID != r1.MessageID {
		t.Errorf("retry got %s, first call %s", r2.MessageID, r1.MessageID)
	}
	email := Message{Key: "k-email", Channel: content.Email, To: "ayse@example.com", Subject: "Sepetin seni bekliyor",
		Text: "Merhaba Ayşe,\n\n<b>%30</b> indirim."}
	if r, err := c.Send(context.Background(), email); err != nil || r.Kind != Sent {
		t.Fatalf("email = %+v (err %v)", r, err)
	}
	msgs := p.Messages()
	if len(msgs) != 2 || msgs[0].Key != smsMsg.Key || msgs[0].Text != "Merhaba Ayşe" {
		t.Fatalf("provider recorded %+v", msgs)
	}
	if e := msgs[1]; e.Subject != email.Subject || e.Text != email.Text || !strings.Contains(e.HTML, "&lt;b&gt;%30&lt;/b&gt; indirim.") {
		t.Errorf("email recorded as %+v", e)
	}
}

func TestClientClassifiesAnswers(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    Result
	}{
		{"202", answer(202, nil, `{"id":"msg_1","status":"accepted"}`), Result{Kind: Sent, Status: 202, MessageID: "msg_1"}},
		{"429 with seconds", answer(429, map[string]string{"Retry-After": "2"}, `{"error":"provider busy"}`),
			Result{Kind: Throttled, Status: 429, RetryAfter: 2 * time.Second, Error: "429 provider busy"}},
		{"429 without Retry-After", answer(429, nil, `{"error":"rate limit exceeded"}`),
			Result{Kind: Throttled, Status: 429, Error: "429 rate limit exceeded"}},
		{"429 with garbage", answer(429, map[string]string{"Retry-After": "soon"}, ``), Result{Kind: Throttled, Status: 429, Error: "429"}},
		{"503", answer(503, nil, `{"error":"service unavailable"}`), Result{Kind: Transient, Status: 503, Error: "503 service unavailable"}},
		{"500 without a body", answer(500, nil, ``), Result{Kind: Transient, Status: 500, Error: "500"}},
		{"400", answer(400, nil, `{"error":"invalid recipient"}`), Result{Kind: Permanent, Status: 400, Error: "400 invalid recipient"}},
		{"422", answer(422, nil, `{}`), Result{Kind: Permanent, Status: 422, Error: "422"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			got, err := client(srv.URL, time.Second).Send(context.Background(), smsMsg)
			if err != nil || got != tt.want {
				t.Errorf("Send = %+v (err %v), want %+v", got, err, tt.want)
			}
		})
	}
}

func answer(status int, headers map[string]string, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestRetryAfterAsADate(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if got := retryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now); got != 3*time.Second {
		t.Errorf("date = %s, want 3s", got)
	}
	if got := retryAfter(now.Add(-time.Hour).Format(http.TimeFormat), now); got != 0 {
		t.Errorf("past date = %s, want 0", got)
	}
}

func TestClientTimeoutAndNetworkErrorsAreTransient(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server notices a client that left only once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	r, err := client(slow.URL, 100*time.Millisecond).Send(context.Background(), smsMsg)
	if err != nil || r.Kind != Transient || r.Status != 0 || r.Error != "no answer within 100ms" {
		t.Errorf("timeout = %+v (err %v)", r, err)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	r, err = client(closed.URL, time.Second).Send(context.Background(), smsMsg)
	if err != nil || r.Kind != Transient || !strings.HasPrefix(r.Error, "network error: ") || strings.Contains(r.Error, closed.URL) {
		t.Errorf("connection refused = %+v (err %v)", r, err)
	}
}

// TestClientReportsCancellation: when the worker stops mid-call, Send
// returns the context's error so that nothing is recorded for the call.
func TestClientReportsCancellation(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client(slow.URL, 5*time.Second).Send(ctx, smsMsg); err == nil {
		t.Error("Send returned no error for a cancelled context")
	}
}

func TestRequestBodies(t *testing.T) {
	var got map[string]string
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(202)
	}))
	defer srv.Close()
	if _, err := client(srv.URL, time.Second).Send(context.Background(), smsMsg); err != nil {
		t.Fatal(err)
	}
	if key != smsMsg.Key || len(got) != 2 || got["to"] != smsMsg.To || got["text"] != smsMsg.Text {
		t.Errorf("SMS request: key %q, body %v", key, got)
	}
}
