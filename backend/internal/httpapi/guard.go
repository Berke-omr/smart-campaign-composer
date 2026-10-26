package httpapi

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
)

// DefaultAllowedHosts are the host names the api answers under when
// HTTP_ALLOWED_HOSTS is not set: the local machine, and "api" for callers on
// the compose network (the smoke test).
var DefaultAllowedHosts = []string{"localhost", "127.0.0.1", "::1", "api"}

// requestGuard keeps other web pages from using the api through the
// marketer's browser (docs/security-risk-register.md, R-02). The api has no
// authentication, so without it any page open in the same browser could
// create, edit and approve campaigns: a request with a text/plain body is a
// CORS "simple request" and reaches the server without a preflight.
//
//   - Host must be an allowed name, which defeats DNS rebinding: a page whose
//     domain resolves to 127.0.0.1 still sends its own domain as Host.
//   - Sec-Fetch-Site, which browsers set and pages cannot, must say the
//     request comes from the api's own origin (or from the user typing the
//     URL). This covers GET too: opening a draft stream generates drafts.
//   - On methods that change state, Origin (when sent) must match Host, for
//     browsers without Sec-Fetch-Site, and a body must be JSON, so a
//     cross-site form or text/plain body cannot get through.
//
// Clients that are not browsers (curl, the smoke test) send neither header
// and are not affected.
func requestGuard(logger *slog.Logger, allowedHosts []string) func(http.Handler) http.Handler {
	if len(allowedHosts) == 0 {
		allowedHosts = DefaultAllowedHosts
	}
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[normalizeHost(h)] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status, reason := check(r, allowed)
			if status == 0 {
				next.ServeHTTP(w, r)
				return
			}
			logger.Warn("request rejected", "reason", reason, "method", r.Method, "path", r.URL.Path,
				"host", r.Host, "origin", r.Header.Get("Origin"), "sec_fetch_site", r.Header.Get("Sec-Fetch-Site"),
				"request_id", middleware.GetReqID(r.Context()))
			httpx.WriteError(w, status, reason)
		})
	}
}

// check returns 0 when the request may pass, else the status and the reason.
func check(r *http.Request, allowed map[string]bool) (int, string) {
	if !allowed[hostName(r.Host)] {
		return http.StatusForbidden, "unknown host"
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return http.StatusForbidden, "cross-site requests are not allowed"
	}
	if !changesState(r.Method) {
		return 0, ""
	}
	if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
		return http.StatusForbidden, "cross-origin requests are not allowed"
	}
	if r.ContentLength != 0 && !isJSON(r.Header.Get("Content-Type")) {
		return http.StatusUnsupportedMediaType, "request body must be application/json"
	}
	return 0, ""
}

func changesState(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// hostName is the Host header without its port, lower-cased, IPv6 without
// brackets.
func hostName(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(h)
	}
	return normalizeHost(hostport)
}

func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(h), "["), "]"))
}

// sameOrigin reports whether an Origin header names the host the request was
// sent to. Browsers leave out default ports in both, and nginx forwards the
// browser's Host unchanged ($http_host). "null" (sandboxed frames, files)
// never matches.
func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}
