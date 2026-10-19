// Package testdb gives integration tests an isolated, migrated database.
//
// Every call creates a brand-new database and drops it when the test ends, so
// tests never share state, even though `go test ./...` runs packages in
// parallel against the same Postgres server.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/db"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
)

// New returns a pool connected to a fresh database with all migrations applied.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := NewEmpty(t)
	if err := db.Migrate(context.Background(), pool, logging.Discard()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// NewEmpty returns a pool connected to a fresh database without migrations.
//
// Without TEST_DATABASE_URL the test is skipped, so plain `go test ./...` works
// on a laptop. `make test` sets REQUIRE_DB_TESTS=1, which turns the skip into a
// failure: a green run there always means the integration tests really ran.
func NewEmpty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("REQUIRE_DB_TESTS") == "1" {
			t.Fatal("REQUIRE_DB_TESTS=1 but TEST_DATABASE_URL is not set")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()

	name := "test_" + randomSuffix(t)
	ident := pgx.Identifier{name}.Sanitize()
	exec(t, url, "CREATE DATABASE "+ident)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		pool.Close()
		exec(t, url, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)")
	})
	return pool
}

func exec(t *testing.T, url, sql string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to test server: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func randomSuffix(t *testing.T) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
