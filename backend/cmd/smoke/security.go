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
