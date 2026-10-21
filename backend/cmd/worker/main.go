// Command worker sends the deliveries that approvals queue: it claims due
// rows, rate-limits them per channel, calls the SMS/email provider and
// records every call (DECISIONS.md D3–D6, D24).
//
// Run one replica: the 100 messages per second per channel limit is kept
// inside this process (D4). It serves its own /metrics (D25).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/db"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/delivery"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/metrics"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	common, errCommon := config.LoadCommon(config.OSLookup)
	dbCfg, errDB := config.LoadDatabase(config.OSLookup)
	cfg, errWorker := config.LoadWorker(config.OSLookup)
	httpCfg, errHTTP := config.LoadHTTP(config.OSLookup)
	if err := errors.Join(errCommon, errDB, errWorker, errHTTP); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("worker", common.LogLevel)

	pool, err := db.Connect(ctx, dbCfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	reg := metrics.NewRegistry()
	callMetrics := metrics.NewWorker(reg)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler(reg, logger))
	srv := &http.Server{Addr: httpCfg.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	w := &delivery.Worker{
		Queue:        delivery.NewQueue(pool, cfg.Lease),
		Client:       &delivery.Client{BaseURL: cfg.ProviderURL, HTTP: &http.Client{Timeout: cfg.ProviderTimeout}},
		Policy:       delivery.DefaultPolicy,
		Logger:       logger,
		PerSecond:    delivery.MessagesPerSecond,
		Senders:      cfg.Senders,
		Batch:        10,
		Poll:         200 * time.Millisecond,
		DrainTimeout: 8 * time.Second, // inside docker stop's 10 s grace period
		Observe:      callMetrics.Observe,
	}
	logger.Info("worker started", "provider", cfg.ProviderURL, "per_second", delivery.MessagesPerSecond,
		"senders_per_channel", cfg.Senders, "provider_timeout", cfg.ProviderTimeout.String(), "lease", cfg.Lease.String())
	// If the metrics server cannot run (its port is taken), the worker stops
	// too rather than send unobserved.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	served := make(chan error, 1)
	go func() {
		err := httpx.ListenAndServe(ctx, srv, httpCfg.ShutdownTimeout, logger)
		if err != nil {
			cancel()
		}
		served <- err
	}()
	w.Run(ctx)
	logger.Info("worker stopped")
	return <-served
}
