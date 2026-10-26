package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// securityHeaders are the headers nginx must send on every UI response
// (docs/security-risk-register.md, R-08), with a part of the value that
// must be present.
var securityHeaders = []struct{ name, contains string }{
	{"Content-Security-Policy", "frame-ancestors 'none'"},
	{"X-Frame-Options", "DENY"},
	{"X-Content-Type-Options", "nosniff"},
	{"Referrer-Policy", "no-referrer"},
	{"Permissions-Policy", "camera=()"},
	{"Cross-Origin-Opener-Policy", "same-origin"},
}

var assetRef = regexp.MustCompile(`/assets/[^"]+\.js`)

// uiHeaders checks the security headers on the page and on one hashed asset:
// the assets location sets its own add_header, which in nginx hides the
// server-level ones unless they are repeated there.
func uiHeaders(ctx context.Context, ui string, c *checker) {
	const sc = "UI"
	index, err := get(ctx, ui+"/")
	if err != nil {
		c.fail(sc, err)
		return
	}
	asset := assetRef.FindString(index.body)
	if asset == "" {
		c.fail(sc, fmt.Errorf("no /assets/*.js reference in index.html"))
		return
	}
	paths := []string{"/", asset, "/campaigns/1"}
	for _, p := range paths {
		r, err := get(ctx, ui+p)
		if err != nil {
			c.fail(sc, err)
			return
		}
		label := p
		if p == asset {
			label = "/assets/*.js"
		}
		var missing []string
		for _, h := range securityHeaders {
			if !strings.Contains(r.header.Get(h.name), h.contains) {
				missing = append(missing, h.name)
			}
		}
		c.equal(sc, label+": security headers missing", missing, []string{})
		c.equal(sc, label+": Server header", r.header.Get("Server"), "nginx")
	}
}

// previewRule is a read-only request body: the preview only counts.
const previewRule = `{"match":"all","conditions":[]}`

// guards checks the api's request guard (R-02), first on the api directly,
// then through nginx with the headers a browser on the UI sends, which shows
// that nginx forwards the Host with its port. ui may be empty.
func guards(ctx context.Context, api, ui string, c *checker) {
	const sc = "R-02"
	type req struct {
		what, base, method, path, host string
		headers                        map[string]string
		body                           string
		want                           int
	}
	reqs := []req{
		{"text/plain body is refused", api, http.MethodPost, "/api/segments/preview", "",
			map[string]string{"Content-Type": "text/plain"}, previewRule, http.StatusUnsupportedMediaType},
		{"foreign Origin is refused", api, http.MethodPost, "/api/segments/preview", "",
			map[string]string{"Content-Type": "application/json", "Origin": "http://attacker.example"}, previewRule, http.StatusForbidden},
		{"cross-site request is refused", api, http.MethodGet, "/api/config", "",
			map[string]string{"Sec-Fetch-Site": "cross-site"}, "", http.StatusForbidden},
		{"foreign Host (DNS rebinding) is refused", api, http.MethodGet, "/api/config", "attacker.example", nil, "", http.StatusForbidden},
	}
	if ui != "" {
		browser := map[string]string{"Content-Type": "application/json", "Origin": "http://localhost:8080", "Sec-Fetch-Site": "same-origin"}
		reqs = append(reqs,
			req{"via nginx: the UI's POST passes", ui, http.MethodPost, "/api/segments/preview", "localhost:8080", browser, previewRule, http.StatusOK},
			req{"via nginx: another localhost port is refused", ui, http.MethodPost, "/api/segments/preview", "localhost:8080",
				map[string]string{"Content-Type": "application/json", "Origin": "http://localhost:3000"}, previewRule, http.StatusForbidden},
		)
	}
	for _, r := range reqs {
		status, err := call(ctx, r.method, r.base+r.path, r.host, r.headers, r.body)
		if err != nil {
			c.fail(sc, err)
			return
		}
		c.equal(sc, r.what, status, r.want)
	}
}

// call sends one request with the given Host (empty: the URL's) and headers.
func call(ctx context.Context, method, url, host string, headers map[string]string, body string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	if host != "" {
		req.Host = host
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

type response struct {
	status int
	header http.Header
	body   string
}

func get(ctx context.Context, url string) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return response{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return response{status: resp.StatusCode, header: resp.Header, body: string(b)}, err
}
