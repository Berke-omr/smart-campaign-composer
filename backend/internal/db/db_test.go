package db_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/db"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
)

func TestConnectGivesUpAndDoesNotLeakPassword(t *testing.T) {
	cfg := config.Database{
		URL:            "postgres://user:s3cret-pw@127.0.0.1:1/nope?sslmode=disable", // nothing listens on port 1
		MaxConns:       1,
		ConnectTimeout: 1 * time.Second,
	}
	start := time.Now()
	_, err := db.Connect(context.Background(), cfg, logging.Discard())
	if err == nil {
		t.Fatal("expected an error for an unreachable database")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Connect took %s, want it to give up after ~1s", elapsed)
	}
	if strings.Contains(err.Error(), "s3cret-pw") {
		t.Errorf("error leaks the password: %v", err)
	}
}

func TestConnectRejectsInvalidURLWithoutEchoingIt(t *testing.T) {
	cfg := config.Database{URL: "postgres://user:s3cret-pw@host:notaport/db", MaxConns: 1, ConnectTimeout: time.Second}
	_, err := db.Connect(context.Background(), cfg, logging.Discard())
	if err == nil || strings.Contains(err.Error(), "s3cret-pw") {
		t.Fatalf("want a generic error without the password, got %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testdb.New(t) // already migrated once
	ctx := context.Background()

	if err := db.Migrate(ctx, pool, logging.Discard()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("users table does not exist after migration")
	}
}

func TestConcurrentMigrationsDoNotRace(t *testing.T) {
	pool := testdb.NewEmpty(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = db.Migrate(ctx, pool, logging.Discard())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("migrator %d: %v", i, err)
		}
	}
}

// TestUserConstraints proves the schema itself rejects inconsistent profiles,
// independent of the seed generator.
func TestUserConstraints(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	const valid = `INSERT INTO users (first_name, last_name, email, phone, city, sms_opt_in, email_opt_in)
	               VALUES ('Ayşe', 'Yılmaz', 'ayse.yilmaz1@example.com', '+905321234567', 'İstanbul', true, true)`
	if _, err := pool.Exec(ctx, valid); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}

	tests := []struct {
		name           string
		sql            string
		wantConstraint string
	}{
		{
			name:           "sms consent without phone",
			sql:            `INSERT INTO users (first_name, last_name, city, sms_opt_in) VALUES ('A', 'B', 'Ankara', true)`,
			wantConstraint: "users_sms_opt_in_needs_phone",
		},
		{
			name:           "email consent without email",
			sql:            `INSERT INTO users (first_name, last_name, city, email_opt_in) VALUES ('A', 'B', 'Ankara', true)`,
			wantConstraint: "users_email_opt_in_needs_email",
		},
		{
			name:           "orders without purchase date",
			sql:            `INSERT INTO users (first_name, last_name, city, order_count, total_spent) VALUES ('A', 'B', 'Ankara', 2, 100)`,
			wantConstraint: "users_purchase_consistency",
		},
		{
			name:           "spend without orders",
			sql:            `INSERT INTO users (first_name, last_name, city, total_spent) VALUES ('A', 'B', 'Ankara', 50)`,
			wantConstraint: "users_spent_consistency",
		},
		{
			name: "activity before creation",
			sql: `INSERT INTO users (first_name, last_name, city, last_cart_add_at, created_at)
			      VALUES ('A', 'B', 'Ankara', now() - interval '10 days', now())`,
			wantConstraint: "users_created_before_activity",
		},
		{
			name:           "non-Turkish mobile number",
			sql:            `INSERT INTO users (first_name, last_name, city, phone) VALUES ('A', 'B', 'Ankara', '+15551234567')`,
			wantConstraint: "users_phone_check",
		},
		{
			name:           "unsupported language",
			sql:            `INSERT INTO users (first_name, last_name, city, language) VALUES ('A', 'B', 'Ankara', 'de')`,
			wantConstraint: "users_language_check",
		},
		{
			name:           "duplicate email",
			sql:            `INSERT INTO users (first_name, last_name, city, email) VALUES ('A', 'B', 'Ankara', 'ayse.yilmaz1@example.com')`,
			wantConstraint: "users_email_key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("want a constraint violation, got %v", err)
			}
			if pgErr.ConstraintName != tt.wantConstraint {
				t.Errorf("violated %q, want %q (%s)", pgErr.ConstraintName, tt.wantConstraint, pgErr.Message)
			}
		})
	}
}
