// Package logging builds the structured (JSON) logger used by every service.
package logging

import (
	"io"
	"log/slog"
	"os"
)

// New returns a JSON logger writing to stdout, tagged with the service name so
// interleaved `docker compose logs` output stays readable.
func New(service string, level slog.Level) *slog.Logger {
	return NewWithWriter(os.Stdout, service, level)
}

func NewWithWriter(w io.Writer, service string, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h).With("service", service)
}

// Discard returns a logger that drops everything (useful in tests).
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
