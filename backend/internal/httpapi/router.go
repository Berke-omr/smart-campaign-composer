// Package httpapi wires the public HTTP API: routes, middleware and handlers.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
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
	// Now is the clock used to anchor relative-time rules; nil means time.Now.
	Now func() time.Time
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
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Logger))
	r.Use(middleware.Recoverer)

	// Liveness: answers as long as the process can serve HTTP. It deliberately
	// does not touch the database (that is what /readyz is for, added in P5).
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/stats/users", func(w http.ResponseWriter, req *http.Request) {
			stats, err := d.UserStats.Stats(req.Context())
			if err != nil {
				internalError(w, req, d.Logger, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, stats)
		})
		r.Get("/segments/fields", d.segmentFields)
		r.Post("/segments/preview", d.segmentPreview)
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
