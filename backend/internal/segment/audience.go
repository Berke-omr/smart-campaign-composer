package segment

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Audience is the size of a segment, overall and per channel.
type Audience struct {
	Total          int64 `json:"total"`
	SMSReachable   int64 `json:"sms_reachable"`
	EmailReachable int64 `json:"email_reachable"`
}

// Channel reachability = consent AND contact data. The schema's CHECK
// constraints already make consent imply contact; spelling both out keeps
// the rule visible where audiences are counted (and, in P4, where messages
// are created). These filters are applied by the backend, never by the rule.
const (
	smsReachableSQL   = "sms_opt_in AND phone IS NOT NULL"
	emailReachableSQL = "email_opt_in AND email IS NOT NULL"
)

// Counter counts audiences in the database.
type Counter struct {
	pool *pgxpool.Pool
}

func NewCounter(pool *pgxpool.Pool) *Counter {
	return &Counter{pool: pool}
}

// Count returns how many users match the rule, and how many of them can be
// reached on each channel.
func (c *Counter) Count(ctx context.Context, v Validated, asOf time.Time) (Audience, error) {
	q := Compile(v, asOf)
	sql := `SELECT count(*),
	               count(*) FILTER (WHERE ` + smsReachableSQL + `),
	               count(*) FILTER (WHERE ` + emailReachableSQL + `)
	        FROM users
	        WHERE ` + q.Where
	var a Audience
	err := c.pool.QueryRow(ctx, sql, q.Args...).Scan(&a.Total, &a.SMSReachable, &a.EmailReachable)
	return a, err
}
