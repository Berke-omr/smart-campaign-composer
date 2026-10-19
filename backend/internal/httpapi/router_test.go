package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
)

func TestHealthz(t *testing.T) {
	h := NewRouter(Deps{Logger: logging.Discard()})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "{\"status\":\"ok\"}\n" {
		t.Errorf("body = %q", body)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	h := NewRouter(Deps{Logger: logging.Discard()})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
