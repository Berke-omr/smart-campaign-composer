package segment

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
)

// Audience is the size of a segment, overall and per channel.
type Audience struct {
	Total          int64 `json:"total"`
	SMSReachable   int64 `json:"sms_reachable"`
	EmailReachable int64 `json:"email_reachable"`
}

// Reach is an audience as the marketer sees it (DECISIONS.md D22): who can be
// reached on each channel, who is left out for lack of consent, and how many
// messages that makes. People without consent are never counted as
// reachable (R4).
type Reach struct {
	Audience
	SMSExcluded   int64 `json:"sms_excluded"`
	EmailExcluded int64 `json:"email_excluded"`
	// ChannelMessages is the number of messages over the selected channels:
	// one person reachable on both channels gets two.
	ChannelMessages int64 `json:"channel_messages"`
}

// Reach derives the per-channel view for the selected channels.
func (a Audience) Reach(channels []content.Channel) Reach {
	r := Reach{
		Audience:      a,
		SMSExcluded:   a.Total - a.SMSReachable,
		EmailExcluded: a.Total - a.EmailReachable,
	}
	for _, ch := range channels {
		switch ch {
		case content.SMS:
			r.ChannelMessages += a.SMSReachable
		case content.Email:
			r.ChannelMessages += a.EmailReachable
		}
	}
	return r
}

// Channel reachability = consent AND contact data. The schema's CHECK
// constraints already make consent imply contact; spelling both out keeps
// the rule visible where audiences are counted and where messages are
// created (Recipients). These filters are applied by the backend, never by
// the rule.
const (
	smsReachableSQL   = "sms_opt_in AND phone IS NOT NULL"
	emailReachableSQL = "email_opt_in AND email IS NOT NULL"
)

// Recipient is one person a channel reaches, with what a message to them
// needs.
type Recipient struct {
	UserID int64
	// Address is the phone number for SMS and the email address for email.
	Address string
	Values  content.Values
}

// Querier runs a query; a pool and a transaction both satisfy it, so the
// approval can read the audience inside its own transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Recipients returns the people of the audience that ch reaches (R4), in id
// order. The approval freezes exactly these people into deliveries.
func Recipients(ctx context.Context, q Querier, v Validated, asOf time.Time, ch content.Channel) ([]Recipient, error) {
	var reach, address string
	switch ch {
	case content.SMS:
		reach, address = smsReachableSQL, "phone"
	case content.Email:
		reach, address = emailReachableSQL, "email"
	default:
		return nil, fmt.Errorf("unknown channel %q", ch)
	}
	c := Compile(v, asOf)
	rows, err := q.Query(ctx, `SELECT id, `+address+`, first_name, last_name, city
	                           FROM users
	                           WHERE `+c.Where+` AND (`+reach+`)
	                           ORDER BY id`, c.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recipient
	for rows.Next() {
		var r Recipient
		var first, last, city string
		if err := rows.Scan(&r.UserID, &r.Address, &first, &last, &city); err != nil {
			return nil, err
		}
		r.Values = content.Values{content.FirstName: first, content.LastName: last, content.City: city}
		out = append(out, r)
	}
	return out, rows.Err()
}

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

// LongestValues returns, for each channel, the longest first name, last name
// and city among the people of the audience reachable on that channel.
// Messages are validated against these values, so a template that fits them
// fits everyone the channel will reach (DECISIONS.md D15). Postgres counts
// characters (char_length) the same way Go counts runes. Ties are broken by
// value, so the result is deterministic. An empty audience yields empty
// values.
func (c *Counter) LongestValues(ctx context.Context, v Validated, asOf time.Time) (map[content.Channel]content.Values, error) {
	q := Compile(v, asOf)
	longest := func(col, reach string) string {
		return `COALESCE((SELECT ` + col + ` FROM a WHERE ` + reach +
			` ORDER BY char_length(` + col + `) DESC, ` + col + ` LIMIT 1), '')`
	}
	sql := `WITH a AS (
	            SELECT first_name, last_name, city,
	                   (` + smsReachableSQL + `) AS sms, (` + emailReachableSQL + `) AS email
	            FROM users
	            WHERE ` + q.Where + `)
	        SELECT ` + longest("first_name", "sms") + `, ` + longest("last_name", "sms") + `, ` + longest("city", "sms") + `,
	               ` + longest("first_name", "email") + `, ` + longest("last_name", "email") + `, ` + longest("city", "email")
	sms, email := content.Values{}, content.Values{}
	var sf, sl, sc, ef, el, ec string
	if err := c.pool.QueryRow(ctx, sql, q.Args...).Scan(&sf, &sl, &sc, &ef, &el, &ec); err != nil {
		return nil, err
	}
	sms[content.FirstName], sms[content.LastName], sms[content.City] = sf, sl, sc
	email[content.FirstName], email[content.LastName], email[content.City] = ef, el, ec
	return map[content.Channel]content.Values{content.SMS: sms, content.Email: email}, nil
}
