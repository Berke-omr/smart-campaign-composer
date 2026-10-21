package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
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

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestReadyz(t *testing.T) {
	rec := serve(t, Deps{DB: fakePinger{}}, http.MethodGet, "/readyz")
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("database answers: status %d body %q, want 200 ok", rec.Code, rec.Body)
	}

	rec = serve(t, Deps{DB: fakePinger{err: errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")}},
		http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("database down: status %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"unavailable"`) || strings.Contains(body, "10.0.0.5") {
		t.Errorf("database down: body %q, want status unavailable without the error's details", body)
	}
}

// TestReadyzWithAStoppedDatabase uses a real pool whose server is gone: the
// pool connects lazily, so only the readiness ping notices.
func TestReadyzWithAStoppedDatabase(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens there any more
	pool, err := pgxpool.New(context.Background(), "postgres://scc:scc@"+addr+"/scc?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	rec := serve(t, Deps{DB: pool}, http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %q", rec.Code, rec.Body)
	}
	// Liveness does not depend on the database.
	if rec := serve(t, Deps{DB: pool}, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("healthz = %d with the database down, want 200", rec.Code)
	}
}

func TestReadyzWithTheDatabase(t *testing.T) {
	pool := testdb.New(t)
	rec := serve(t, Deps{DB: pool}, http.MethodGet, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body)
	}
}

func TestMetricsRoute(t *testing.T) {
	if rec := serve(t, Deps{}, http.MethodGet, "/metrics"); rec.Code != http.StatusNotFound {
		t.Errorf("without a metrics handler: %d, want 404", rec.Code)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("scc_up 1\n")) })
	rec := serve(t, Deps{Metrics: h}, http.MethodGet, "/metrics")
	if rec.Code != http.StatusOK || rec.Body.String() != "scc_up 1\n" {
		t.Errorf("metrics: %d %q", rec.Code, rec.Body)
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

func TestBriefExamples(t *testing.T) {
	rec := serve(t, Deps{}, http.MethodGet, "/api/briefs/examples")
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"examples\":[]}\n" {
		t.Fatalf("no examples: status %d body %q", rec.Code, rec.Body)
	}

	cat, err := mock.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	rec = serve(t, Deps{Examples: cat.Examples()}, http.MethodGet, "/api/briefs/examples")
	var got struct {
		Examples []mock.Example `json:"examples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Examples) != 5 || got.Examples[0].Title != "Cart reminder — İstanbul" || got.Examples[0].Tone != "friendly" {
		t.Errorf("examples = %+v", got.Examples)
	}
}
