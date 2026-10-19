// Command seed fills the users table with deterministic fake profiles.
//
// It is idempotent: when users already exist it does nothing. Pass -reset to
// empty the table and seed again (`make reseed`).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/db"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
)

func main() {
	reset := flag.Bool("reset", false, "delete existing users and seed again")
	flag.Parse()

	if err := run(*reset); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run(reset bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	common, errCommon := config.LoadCommon(config.OSLookup)
	dbCfg, errDB := config.LoadDatabase(config.OSLookup)
	seedCfg, errSeed := config.LoadSeed(config.OSLookup)
	if err := errors.Join(errCommon, errDB, errSeed); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := logging.New("seed", common.LogLevel)

	pool, err := db.Connect(ctx, dbCfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	start := time.Now()
	users := seed.Generate(seedCfg.Users, seedCfg.RandomSeed, start)
	res, err := seed.Load(ctx, pool, users, reset)
	if err != nil {
		return err
	}
	if res.Skipped {
		logger.Info("users already present, skipping (use -reset to reseed)")
		return nil
	}
	logger.Info("seeded users",
		"inserted", res.Inserted,
		"random_seed", seedCfg.RandomSeed,
		"reset", reset,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}
