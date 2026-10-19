package seed

import (
	"context"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Result reports what Load did.
type Result struct {
	Inserted int64
	Skipped  bool // the table already had users and reset was false
}

var columns = []string{
	"first_name", "last_name", "email", "phone", "city", "language",
	"sms_opt_in", "email_opt_in", "last_cart_add_at", "last_purchase_at",
	"order_count", "total_spent", "created_at",
}

// Load writes users with a single COPY. It is idempotent: if the table already
// has rows it does nothing, unless reset is true, in which case users and
// campaigns are emptied first (ids restart at 1, so a reset gives the same ids
// as a fresh install). Campaigns go too: their deliveries point at users, and
// a frozen audience of people who no longer exist means nothing.
func Load(ctx context.Context, pool *pgxpool.Pool, users []User, reset bool) (Result, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	// Serialize concurrent seeders: a second one waits here, then sees the
	// rows written by the first and skips.
	if _, err := tx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return Result{}, fmt.Errorf("lock users: %w", err)
	}

	if reset {
		if _, err := tx.Exec(ctx, "TRUNCATE users, campaigns RESTART IDENTITY CASCADE"); err != nil {
			return Result{}, fmt.Errorf("truncate users and campaigns: %w", err)
		}
	} else {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users)").Scan(&exists); err != nil {
			return Result{}, fmt.Errorf("check users: %w", err)
		}
		if exists {
			return Result{Skipped: true}, nil
		}
	}

	n, err := tx.CopyFrom(ctx, pgx.Identifier{"users"}, columns, pgx.CopyFromSlice(len(users), func(i int) ([]any, error) {
		u := users[i]
		return []any{
			u.FirstName, u.LastName, u.Email, u.Phone, u.City, u.Language,
			u.SMSOptIn, u.EmailOptIn, u.LastCartAddAt, u.LastPurchaseAt,
			u.OrderCount, kurusToNumeric(u.TotalSpentKurus), u.CreatedAt,
		}, nil
	}))
	if err != nil {
		return Result{}, fmt.Errorf("copy users: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit: %w", err)
	}
	return Result{Inserted: n}, nil
}

// kurusToNumeric converts minor units to NUMERIC(12,2) exactly (12345 → 123.45).
func kurusToNumeric(kurus int64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(kurus), Exp: -2, Valid: true}
}
