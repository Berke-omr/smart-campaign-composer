// Package httpapi wires the public HTTP API: routes, middleware and handlers.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/campaign"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

// Deps are the collaborators the handlers need. Interfaces keep handlers
// testable without a database.
type Deps struct {
	Logger    *slog.Logger
	UserStats UserStatsReader
	Segments  *segment.Registry
	Audience  AudienceCounter
	LLM       LLMInfo
	// Examples are the brief library: the mock catalog's example briefs.
	Examples []mock.Example
	// Campaigns runs the draft flow; nil disables the campaign routes.
	Campaigns *campaign.Service
	// Now is the clock used to anchor relative-time rules and to time
	// dashboard snapshots; nil means time.Now.
	Now func() time.Time
	// Tracking feeds the live dashboard stream; nil leaves the route out.
	Tracking Snapshotter
	// SnapshotEvery is the dashboard stream's interval; 0 means one second.
	SnapshotEvery time.Duration
	// Stopping is cancelled when the server begins to shut down, and every
	// open stream (drafts, dashboard) ends then; the browser reconnects to
	// the next api. nil: streams end only when their client leaves.
	Stopping context.Context
	// DB is pinged by /readyz; nil leaves the route out.
	DB Pinger
	// Metrics serves /metrics (Prometheus); nil leaves the route out.
	Metrics http.Handler
	// AllowedHosts are the host names /api answers under (requestGuard);
	// empty means DefaultAllowedHosts.
	AllowedHosts []string
}

// Pinger checks that the database answers. *pgxpool.Pool implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// readyTimeout bounds the readiness ping, so a database that hangs makes
// /readyz fail instead of hang.
const readyTimeout = 2 * time.Second

// LLMInfo tells the UI which LLM is active, so it can show a demo badge when
// the mock answers (DECISIONS.md D12).
type LLMInfo struct {
	Mode  string  `json:"mode"`  // "anthropic" or "mock"
	Model *string `json:"model"` // null for the mock
}

type UserStatsReader interface {
	Stats(ctx context.Context) (users.Stats, error)
}

type AudienceCounter interface {
	Count(ctx context.Context, v segment.Validated, asOf time.Time) (segment.Audience, error)
}

// NewRouter returns the API's root handler.
func NewRouter(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.SnapshotEvery <= 0 {
		d.SnapshotEvery = time.Second
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Logger))
	r.Use(middleware.Recoverer)

	// Liveness: answers as long as the process can serve HTTP. It deliberately
	// does not touch the database (that is what /readyz is for).
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if d.Metrics != nil {
		r.Method(http.MethodGet, "/metrics", d.Metrics)
	}
	// Readiness: the api can do its work only while the database answers.
	if d.DB != nil {
		r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
			ctx, cancel := context.WithTimeout(req.Context(), readyTimeout)
			defer cancel()
			if err := d.DB.Ping(ctx); err != nil {
				d.Logger.Warn("not ready: database does not answer", "err", err)
				httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": "unavailable", "error": "database unreachable"})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
	}

	// Probes and /metrics stay outside the guard: they change nothing and are
	// called by Docker and scrapers with their own Host.
	r.Route("/api", func(r chi.Router) {
		r.Use(requestGuard(d.Logger, d.AllowedHosts))
		r.Get("/stats/users", func(w http.ResponseWriter, req *http.Request) {
			stats, err := d.UserStats.Stats(req.Context())
			if err != nil {
				internalError(w, req, d.Logger, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, stats)
		})
		r.Get("/config", func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteJSON(w, http.StatusOK, map[string]LLMInfo{"llm": d.LLM})
		})
		r.Get("/briefs/examples", func(w http.ResponseWriter, _ *http.Request) {
			examples := d.Examples
			if examples == nil {
				examples = []mock.Example{}
			}
			httpx.WriteJSON(w, http.StatusOK, map[string][]mock.Example{"examples": examples})
		})
		r.Get("/segments/fields", d.segmentFields)
		r.Post("/segments/preview", d.segmentPreview)

		if d.Campaigns != nil {
			r.Post("/campaigns", d.createCampaign)
			r.Get("/campaigns", d.listCampaigns)
			r.Get("/campaigns/{id}", d.getCampaign)
			r.Get("/campaigns/{id}/draft/stream", d.streamDrafts)
			r.Put("/campaigns/{id}/messages/{channel}", d.editMessage)
			r.Post("/campaigns/{id}/approve", d.approveCampaign)
		}
		if d.Tracking != nil {
			r.Get("/campaigns/{id}/events", d.streamEvents)
		}
	})

	return r
}

// internalError logs the real cause and returns a generic message, so database
// details never reach the client.
func internalError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	logger.Error("request failed", "path", r.URL.Path, "request_id", middleware.GetReqID(r.Context()), "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal error")
}

// requestLogger logs one line per request. Health checks are skipped because
// Docker polls them every few seconds and they would drown everything else.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
