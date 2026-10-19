// Package mockprovider implements the fake SMS/email provider.
//
// In P4 it gains configurable latency, 5xx and 429 responses, its own rate
// limit and Idempotency-Key deduplication. For now it only answers /healthz.
package mockprovider

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
)

func NewHandler() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return r
}
