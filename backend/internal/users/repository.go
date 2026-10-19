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

// DistinctCities returns every city present in the data, most common first.
// It feeds the segment registry, so the LLM can only pick cities that exist.
func (r *Repository) DistinctCities(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT city FROM users GROUP BY city ORDER BY count(*) DESC, city`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cities []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cities = append(cities, c)
	}
	return cities, rows.Err()
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
