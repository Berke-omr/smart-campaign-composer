// Package httpx contains small HTTP helpers shared by the api and the mock provider.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve runs srv on ln until ctx is cancelled, then shuts it down gracefully:
// it stops accepting new connections and waits up to shutdownTimeout for
// in-flight requests to finish.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", ln.Addr().String())
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		// Serve only returns before shutdown if something went wrong.
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down http server", "timeout", shutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ListenAndServe is Serve on a freshly opened TCP listener for srv.Addr.
func ListenAndServe(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	return Serve(ctx, srv, ln, shutdownTimeout, logger)
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ErrorBody is the shape of every error response.
type ErrorBody struct {
	Error string `json:"error"`
}

// WriteError writes {"error": msg} with the given status code.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorBody{Error: msg})
}
