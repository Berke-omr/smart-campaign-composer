package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

type fakeStats struct {
	stats users.Stats
	err   error
}

func (f fakeStats) Stats(context.Context) (users.Stats, error) { return f.stats, f.err }

func serve(t *testing.T, d Deps, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	if d.Logger == nil {
		d.Logger = logging.Discard()
	}
	rec := httptest.NewRecorder()
	NewRouter(d).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := serve(t, Deps{}, http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "{\"status\":\"ok\"}\n" {
		t.Errorf("body = %q", body)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	rec := serve(t, Deps{}, http.MethodGet, "/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUserStats(t *testing.T) {
	d := Deps{UserStats: fakeStats{stats: users.Stats{Total: 5000, SMSOptIn: 3156, EmailOptIn: 3714}}}
	rec := serve(t, d, http.MethodGet, "/api/stats/users")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"total":5000,"sms_opt_in":3156,"email_opt_in":3714}` + "\n"
	if body := rec.Body.String(); body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestUserStatsErrorDoesNotLeakDetails(t *testing.T) {
	d := Deps{UserStats: fakeStats{err: errors.New(`relation "users" does not exist`)}}
	rec := serve(t, d, http.MethodGet, "/api/stats/users")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "relation") {
		t.Errorf("response leaks the database error: %s", rec.Body.String())
	}
}

func TestUserStatsRejectsWrongMethod(t *testing.T) {
	rec := serve(t, Deps{UserStats: fakeStats{}}, http.MethodPost, "/api/stats/users")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestConfigReportsLLMMode(t *testing.T) {
	model := "claude-sonnet-5-5"
	tests := []struct {
		name string
		llm  LLMInfo
		want string
	}{
		{"mock", LLMInfo{Mode: "mock"}, `{"llm":{"mode":"mock","model":null}}` + "\n"},
		{"anthropic", LLMInfo{Mode: "anthropic", Model: &model}, `{"llm":{"mode":"anthropic","model":"claude-sonnet-5-5"}}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, Deps{LLM: tt.llm}, http.MethodGet, "/api/config")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if body := rec.Body.String(); body != tt.want {
				t.Errorf("body = %q, want %q", body, tt.want)
			}
		})
	}
}
