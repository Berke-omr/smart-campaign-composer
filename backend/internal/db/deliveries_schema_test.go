package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
	"github.com/Berke-omr/smart-campaign-composer/backend/migrations"
)

// TestDeliveryConstraints proves the schema itself keeps the delivery ledger
// consistent: one message per person and channel, rendered messages within
// the channel limits, and lease/sent bookkeeping that cannot disagree with
// the status (DECISIONS.md D5, D15, D24).
func TestDeliveryConstraints(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	var userID, campaignID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (first_name, last_name, email, phone, city, sms_opt_in, email_opt_in)
		VALUES ('Ayşe', 'Yılmaz', 'ayse@example.com', '+905321234567', 'İstanbul', true, true)
		RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO campaigns (name, brief, tone, channels, segment_rule, audience_as_of, llm_mode)
		VALUES ('n', 'b', 'friendly', ARRAY['sms', 'email'], '{}', now(), 'mock')
		RETURNING id`).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}

	// 160 characters that take 320 bytes: the limit counts characters.
	sms160 := strings.Repeat("ş", 160)
	for name, sql := range map[string]string{
		"SMS of 160 characters": `INSERT INTO deliveries (campaign_id, user_id, channel, recipient, text)
		                          VALUES ($1, $2, 'sms', '+905321234567', '` + sms160 + `')`,
		"email": `INSERT INTO deliveries (campaign_id, user_id, channel, recipient, subject, body)
		          VALUES ($1, $2, 'email', 'ayse@example.com', 'Konu', 'Satır 1
Satır 2')`,
	} {
		if _, err := pool.Exec(ctx, sql, campaignID, userID); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE deliveries SET status = 'sending', locked_until = now() + interval '30 seconds', attempts = 1
	                             WHERE channel = 'sms'`); err != nil {
		t.Fatalf("claim rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET status = 'sending', approved_at = now() WHERE id = $1`, campaignID); err != nil {
		t.Fatalf("approval rejected: %v", err)
	}

	insert := func(channel, text, subject, body string) string {
		return `INSERT INTO deliveries (campaign_id, user_id, channel, recipient, text, subject, body)
		        VALUES ($1, $2, '` + channel + `', 'x', ` + text + `, ` + subject + `, ` + body + `)`
	}
	tests := []struct {
		name           string
		sql            string
		wantConstraint string
	}{
		{"same person and channel twice", insert("sms", "'again'", "NULL", "NULL"), "deliveries_campaign_id_user_id_channel_key"},
		{"SMS of 161 characters", `DELETE FROM deliveries WHERE channel = 'sms'; ` + insert("sms", "'"+sms160+"x'", "NULL", "NULL"), "deliveries_shape"},
		{"SMS with a subject", `DELETE FROM deliveries WHERE channel = 'sms'; ` + insert("sms", "'t'", "'s'", "NULL"), "deliveries_shape"},
		{"subject of 81 characters", `DELETE FROM deliveries WHERE channel = 'email'; ` + insert("email", "NULL", "'"+strings.Repeat("a", 81)+"'", "'b'"), "deliveries_shape"},
		{"subject on two lines", `DELETE FROM deliveries WHERE channel = 'email'; ` + insert("email", "NULL", "E'a\\nb'", "'b'"), "deliveries_shape"},
		{"email without a body", `DELETE FROM deliveries WHERE channel = 'email'; ` + insert("email", "NULL", "'s'", "NULL"), "deliveries_shape"},
		{"unknown status", `UPDATE deliveries SET status = 'delivered' WHERE channel = 'email'`, "deliveries_status_check"},
		{"sending without a lease", `UPDATE deliveries SET status = 'sending' WHERE channel = 'email'`, "deliveries_lease"},
		{"lease while queued", `UPDATE deliveries SET locked_until = now() WHERE channel = 'email'`, "deliveries_lease"},
		{"sent without a time", `UPDATE deliveries SET status = 'sent', locked_until = NULL WHERE channel = 'sms'`, "deliveries_sent"},
		{"more 429s than calls", `UPDATE deliveries SET throttled = 2 WHERE channel = 'sms'`, "deliveries_throttled_within_attempts"},
		{"empty recipient", `UPDATE deliveries SET recipient = '' WHERE channel = 'sms'`, "deliveries_recipient_check"},
		{"retry without a due time", `INSERT INTO delivery_attempts (delivery_id, campaign_id, channel, attempt, started_at, finished_at, outcome)
		                              SELECT id, campaign_id, channel, 1, now(), now(), 'retry' FROM deliveries WHERE channel = 'sms'`, "delivery_attempts_retry_due"},
		{"campaign sending without approval time", `UPDATE campaigns SET approved_at = NULL WHERE id = $1`, "campaigns_approved"},
		{"campaign completed without finish time", `UPDATE campaigns SET status = 'completed' WHERE id = $1`, "campaigns_finished"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case runs in a transaction that is rolled back, so the
			// cases cannot affect each other.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			for _, stmt := range strings.Split(tt.sql, "; ") {
				var args []any
				switch strings.Count(stmt, "$") {
				case 1:
					args = []any{campaignID}
				case 2:
					args = []any{campaignID, userID}
				}
				if _, err = tx.Exec(ctx, stmt, args...); err != nil {
					break
				}
			}
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

// TestDeliveriesMigrationGoesDownAndUp checks that migration 3 can be rolled
// back and applied again.
func TestDeliveriesMigrationGoesDownAndUp(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 2); err != nil {
		t.Fatalf("down: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.deliveries') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("deliveries still exists after down (err %v)", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.delivery_attempts') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Fatalf("delivery_attempts missing after up (err %v)", err)
	}
}
