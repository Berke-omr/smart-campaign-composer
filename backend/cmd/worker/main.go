// Command worker sends the deliveries that approvals queue: it claims due
// rows, rate-limits them per channel, calls the SMS/email provider and
// records every call (DECISIONS.md D3–D6, D24).
//
// Run one replica: the 100 messages per second per channel limit is kept
// inside this process (D4).
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
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
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
	if err := errors.Join(errCommon, errDB, errWorker); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("worker", common.LogLevel)

	pool, err := db.Connect(ctx, dbCfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

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
	}
	logger.Info("worker started", "provider", cfg.ProviderURL, "per_second", delivery.MessagesPerSecond,
		"senders_per_channel", cfg.Senders, "provider_timeout", cfg.ProviderTimeout.String(), "lease", cfg.Lease.String())
	w.Run(ctx)
	logger.Info("worker stopped")
	return nil
}
