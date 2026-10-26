package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
)

// localRequest is httptest.NewRequest addressed to localhost, as the
// browser and the api's own clients send it (httptest's default Host,
// example.com, is not an allowed host).
func localRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "localhost:8080"
	return r
}

type guardCase struct {
	name    string
	method  string
	host    string
	headers map[string]string
	body    string
	chunked bool // body of unknown length (Transfer-Encoding: chunked)
	want    int  // 200: the request reached the handler
}

func runGuard(t *testing.T, allowed []string, tt guardCase) {
	t.Helper()
	called := false
	h := requestGuard(logging.Discard(), allowed)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	var body io.Reader
	if tt.body != "" {
		body = strings.NewReader(tt.body)
	}
	method := tt.method
	if method == "" {
		method = http.MethodPost
	}
	req := httptest.NewRequest(method, "/api/campaigns/1/approve", body)
	req.Host = tt.host
	if req.Host == "" {
		req.Host = "localhost:8080"
	}
	if tt.chunked {
		req.ContentLength = -1
	}
	for k, v := range tt.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != tt.want {
		t.Fatalf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
	}
	if called != (tt.want == http.StatusOK) {
		t.Fatalf("handler called = %v with status %d", called, rec.Code)
	}
	if tt.want != http.StatusOK {
		var e httpx.ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error == "" {
			t.Errorf("error body %q is not {\"error\": ...}", rec.Body)
		}
	}
}

func TestGuardLetsTheAppAndNonBrowserClientsThrough(t *testing.T) {
	sameOrigin := map[string]string{"Origin": "http://localhost:8080", "Sec-Fetch-Site": "same-origin"}
	jsonCT := map[string]string{"Content-Type": "application/json"}
	with := func(a, b map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range a {
			m[k] = v
		}
		for k, v := range b {
			m[k] = v
		}
		return m
	}
	for _, tt := range []guardCase{
		{name: "the UI's JSON POST", headers: with(sameOrigin, jsonCT), body: `{}`, want: 200},
		{name: "JSON with a charset", headers: with(sameOrigin, map[string]string{"Content-Type": "application/json; charset=utf-8"}), body: `{}`, want: 200},
		{name: "the UI's approve: POST without a body or Content-Type", headers: sameOrigin, want: 200},
		{name: "the UI's PUT", method: http.MethodPut, headers: with(sameOrigin, jsonCT), body: `{}`, want: 200},
		{name: "curl or the smoke test: no Origin, no Sec-Fetch-Site", host: "api:8080", headers: jsonCT, body: `{}`, want: 200},
		{name: "a bodiless POST from curl", want: 200},
		{name: "the UI's draft stream (EventSource, same origin)", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, want: 200},
		{name: "a URL typed into the address bar", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Site": "none"}, want: 200},
		{name: "default port: Origin and Host without a port", host: "localhost", headers: map[string]string{"Origin": "http://localhost"}, want: 200},
		{name: "host names are case-insensitive", host: "LocalHost:8080", headers: map[string]string{"Origin": "http://localhost:8080"}, want: 200},
	} {
		t.Run(tt.name, func(t *testing.T) { runGuard(t, nil, tt) })
	}
}

func TestGuardRejectsCrossSiteRequests(t *testing.T) {
	for _, tt := range []guardCase{
		// The attack of the audit (R-02): a page posts a "simple request".
		{name: "cross-site text/plain POST", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example", "Content-Type": "text/plain"}, body: `{"brief":"x"}`, want: 403},
		{name: "cross-site bodiless approve", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"}, want: 403},
		{name: "another port on localhost is same-site, not same-origin", headers: map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://localhost:3000"}, want: 403},
		// Opening a draft stream generates drafts, so a cross-site GET is refused too.
		{name: "cross-site GET of a draft stream", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: 403},
		{name: "same-site GET", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Site": "same-site"}, want: 403},
		// Browsers without Sec-Fetch-Site still send Origin on a cross-origin POST.
		{name: "foreign Origin without Sec-Fetch-Site", headers: map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json"}, body: `{}`, want: 403},
		{name: "Origin on another port of the same host", headers: map[string]string{"Origin": "http://localhost:3000", "Content-Type": "application/json"}, body: `{}`, want: 403},
		{name: "Origin null (sandboxed frame or file)", headers: map[string]string{"Origin": "null"}, want: 403},
		// A body must be JSON, which a page can only send after a CORS preflight.
		{name: "text/plain body", headers: map[string]string{"Content-Type": "text/plain"}, body: `{}`, want: 415},
		{name: "form body", headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, body: `a=b`, want: 415},
		{name: "body without Content-Type", body: `{}`, want: 415},
		{name: "chunked text/plain body", headers: map[string]string{"Content-Type": "text/plain"}, body: `{}`, chunked: true, want: 415},
	} {
		t.Run(tt.name, func(t *testing.T) { runGuard(t, nil, tt) })
	}
}

func TestGuardChecksTheHost(t *testing.T) {
	get := func(name, host string, want int) guardCase {
		return guardCase{name: name, method: http.MethodGet, host: host, want: want}
	}
	for _, tt := range []guardCase{
		get("localhost via nginx", "localhost:8080", 200),
		get("the api port", "localhost:8081", 200),
		get("127.0.0.1", "127.0.0.1:8080", 200),
		get("IPv6 loopback", "[::1]:8080", 200),
		get("compose network", "api:8080", 200),
		// DNS rebinding: the page's domain points at 127.0.0.1 but stays in Host.
		get("rebinding domain", "attacker.example", 403),
		get("rebinding domain with a port", "attacker.example:8080", 403),
		get("trailing dot", "localhost.:8080", 403),
		get("LAN address", "192.0.2.10:8080", 403),
	} {
		t.Run(tt.name, func(t *testing.T) { runGuard(t, nil, tt) })
	}

	t.Run("configured hosts replace the defaults", func(t *testing.T) {
		allowed := []string{"composer.example.com", "[::1]"}
		runGuard(t, allowed, get("", "composer.example.com", 200))
		runGuard(t, allowed, get("", "[::1]:8080", 200))
		runGuard(t, allowed, get("", "localhost:8080", 403))
	})
}

func TestGuardCoversOnlyTheAPI(t *testing.T) {
	d := Deps{
		Logger:  logging.Discard(),
		Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	}
	for _, tt := range []struct {
		path string
		want int
	}{
		{"/api/config", http.StatusForbidden},
		{"/healthz", http.StatusOK},
		{"/metrics", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		req.Host = "attacker.example"
		rec := httptest.NewRecorder()
		NewRouter(d).ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s with a foreign Host: status %d, want %d", tt.path, rec.Code, tt.want)
		}
	}
}
