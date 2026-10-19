package httpx

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]int{"n": 1})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if body := rec.Body.String(); body != "{\"n\":1}\n" {
		t.Errorf("body = %q", body)
	}
}

func TestServeShutsDownWhenContextIsCancelled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, time.Second, logging.Discard()) }()

	resp, err := http.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("server not reachable: %v", err)
	}
	resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after context cancellation")
	}
}
