// Package db opens the PostgreSQL connection pool and applies migrations.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/migrations"
)

// Connect opens a pool and waits until the database answers, retrying with
// exponential backoff up to cfg.ConnectTimeout. Compose health checks already
// order startup, but retrying here keeps services robust when the database
// restarts or is slow to accept connections.
func Connect(ctx context.Context, cfg config.Database, logger *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// Do not wrap err: it may echo the URL, which contains the password.
		return nil, fmt.Errorf("invalid DATABASE_URL")
	}
	poolCfg.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	deadline := time.Now().Add(cfg.ConnectTimeout)
	backoff := 250 * time.Millisecond
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			logger.Info("connected to database", "host", poolCfg.ConnConfig.Host, "database", poolCfg.ConnConfig.Database)
			return pool, nil
		}
		if time.Now().Add(backoff).After(deadline) {
			pool.Close()
			return nil, fmt.Errorf("database not reachable within %s: %w", cfg.ConnectTimeout, err)
		}
		logger.Warn("database not ready, retrying", "retry_in", backoff.String(), "err", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

// Migrate applies all pending migrations. A Postgres advisory lock makes
// concurrent runs safe: a second migrator waits instead of racing.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	// Retry the lock every second for up to two minutes (goose's default polls
	// every 5s, which makes a waiting migrator needlessly slow).
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 120))
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		logger.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration_ms", r.Duration.Milliseconds())
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	logger.Info("schema up to date", "version", version, "applied_now", len(results))
	return nil
}
