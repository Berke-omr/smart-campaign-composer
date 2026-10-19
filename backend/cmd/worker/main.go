// Command worker will dispatch queued deliveries to the SMS/email provider (P4).
//
// For now it only proves the wiring: it connects to the database, reports a
// heartbeat and shuts down cleanly on SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/db"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
)

const heartbeatInterval = 30 * time.Second

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
	if err := errors.Join(errCommon, errDB); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("worker", common.LogLevel)

	pool, err := db.Connect(ctx, dbCfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	logger.Info("worker started (placeholder until P4)")
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopping")
			return nil
		case <-ticker.C:
			if err := pool.Ping(ctx); err != nil {
				logger.Warn("heartbeat: database unreachable", "err", err)
				continue
			}
			logger.Debug("heartbeat")
		}
	}
}
