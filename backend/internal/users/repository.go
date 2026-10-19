// Package users reads the user base.
package users

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Stats summarizes how many users exist and how many can be reached per channel.
type Stats struct {
	Total      int64 `json:"total"`
	SMSOptIn   int64 `json:"sms_opt_in"`
	EmailOptIn int64 `json:"email_opt_in"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := r.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE sms_opt_in),
		       count(*) FILTER (WHERE email_opt_in)
		FROM users`).Scan(&s.Total, &s.SMSOptIn, &s.EmailOptIn)
	return s, err
}
