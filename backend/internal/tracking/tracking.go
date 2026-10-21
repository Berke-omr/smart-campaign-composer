// Package tracking reads the live state of a sending campaign for the
// dashboard (DECISIONS.md D7, D25): counts per channel and status, provider
// calls per second over the last 30 seconds, and the latest issues. A
// snapshot always carries the whole state, so a client that reconnects needs
// nothing but the next one.
package tracking

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/delivery"
)

const (
	// WindowSeconds is the length of the sends-per-second series.
	WindowSeconds = 30
	// Settle keeps the newest seconds out of the series until they are
	// complete. A call is logged when it finishes, 20–120 ms after it
	// started, so the second that has just ended still misses some calls.
	Settle = time.Second
	// MaxIssues is how many recent issues a snapshot lists.
	MaxIssues = 10
)

var ErrNotFound = errors.New("campaign not found")

// Snapshot is a campaign's live state at one moment.
type Snapshot struct {
	CampaignID int64      `json:"campaign_id"`
	Status     string     `json:"status"`
	ApprovedAt *time.Time `json:"approved_at"`
	FinishedAt *time.Time `json:"finished_at"`
	// At is when the snapshot was taken, on the api's clock.
	At    time.Time `json:"at"`
	Total int       `json:"total"`
	// Done counts the messages that are final: sent or failed.
	Done   int    `json:"done"`
	Window Window `json:"window"`
	// Channels holds the campaign's selected channels only.
	Channels     map[content.Channel]*Channel `json:"channels"`
	RecentIssues []Issue                      `json:"recent_issues"`
}

// Window is the span of the per-second series: WindowSeconds one-second
// buckets, from inclusive, to exclusive, aligned to whole seconds.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Channel is one channel's messages by state and its sending speed.
type Channel struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Queued   int `json:"queued"`
	Sending  int `json:"sending"`
	Retrying int `json:"retrying"`
	Sent     int `json:"sent"`
	Failed   int `json:"failed"`
	// PerSecond[i] is the number of provider calls (retries included)
	// started in the i-th second of the window, oldest first.
	PerSecond []int `json:"per_second"`
	// Rate is the newest second of PerSecond while the campaign sends, and
	// 0 once it is finished.
	Rate  int `json:"rate"`
	Limit int `json:"limit"`
}

// Issue is a provider call that did not end in a sent message.
type Issue struct {
	// At is when the answer came, or when the worker stopped waiting.
	At      time.Time       `json:"at"`
	Channel content.Channel `json:"channel"`
	// Kind is throttled (429), provider_error (5xx), rejected (other 4xx) or
	// no_answer (a timeout or a network error).
	Kind string `json:"kind"`
	// Code is the HTTP status; null when there was no answer.
	Code *int `json:"code"`
	// Outcome is retry or failed (the message will not be sent).
	Outcome string `json:"outcome"`
	// RetryInMS is how long after this call the retry was scheduled; null
	// when the message failed.
	RetryInMS *int64 `json:"retry_in_ms"`
	Attempt   int    `json:"attempt"`
	Error     string `json:"error"`
}

// Final reports whether the campaign has finished sending.
func (s Snapshot) Final() bool { return s.Status == "completed" || s.Status == "failed" }

// Reader reads snapshots from the database.
type Reader struct {
	pool *pgxpool.Pool
}

func NewReader(pool *pgxpool.Pool) *Reader { return &Reader{pool: pool} }

// Snapshot reads campaign id's state as of now. Every part is read in one
// repeatable-read transaction, so the counts, the status and the series
// belong to the same moment of the database.
//
// now is the api's clock. The series' window is computed from it and
// compared only with started_at, which the worker takes from its own clock;
// the database's now() is never mixed in (it runs a few milliseconds behind
// the host under Docker on a Mac, D24).
func (r *Reader) Snapshot(ctx context.Context, id int64, now time.Time) (Snapshot, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback(ctx) // read-only: nothing to commit

	s := Snapshot{CampaignID: id, At: now.UTC(), Channels: map[content.Channel]*Channel{}, RecentIssues: []Issue{}}
	var channels []string
	err = tx.QueryRow(ctx, `SELECT status, channels, approved_at, finished_at FROM campaigns WHERE id = $1`, id).
		Scan(&s.Status, &channels, &s.ApprovedAt, &s.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	for _, ch := range channels {
		s.Channels[content.Channel(ch)] = &Channel{PerSecond: make([]int, WindowSeconds), Limit: delivery.MessagesPerSecond}
	}

	if err := s.readCounts(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	if err := s.readSpeed(ctx, tx, now); err != nil {
		return Snapshot{}, err
	}
	if err := s.readIssues(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

func (s *Snapshot) readCounts(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `
		SELECT channel, status, count(*) FROM deliveries WHERE campaign_id = $1 GROUP BY channel, status`, s.CampaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ch, status string
		var n int
		if err := rows.Scan(&ch, &status, &n); err != nil {
			return err
		}
		c := s.Channels[content.Channel(ch)]
		if c == nil {
			continue // not a selected channel; cannot happen after approval
		}
		switch status {
		case "queued":
			c.Queued = n
		case "sending":
			c.Sending = n
		case "retrying":
			c.Retrying = n
		case "sent":
			c.Sent = n
		case "failed":
			c.Failed = n
		}
		c.Total += n
		s.Total += n
	}
	for _, c := range s.Channels {
		c.Done = c.Sent + c.Failed
		s.Done += c.Done
	}
	return rows.Err()
}

// readSpeed fills each channel's per-second series. While the campaign
// sends, the window ends Settle before the current second; once it is
// finished, the window ends with its last call, so the chart keeps showing
// how the campaign went out.
func (s *Snapshot) readSpeed(ctx context.Context, tx pgx.Tx, now time.Time) error {
	to := now.Truncate(time.Second).Add(-Settle)
	if s.Final() {
		var last *time.Time
		if err := tx.QueryRow(ctx, `SELECT max(started_at) FROM delivery_attempts WHERE campaign_id = $1`,
			s.CampaignID).Scan(&last); err != nil {
			return err
		}
		if last != nil {
			to = last.Truncate(time.Second).Add(time.Second)
		}
	}
	from := to.Add(-WindowSeconds * time.Second)
	s.Window = Window{From: from.UTC(), To: to.UTC()}

	rows, err := tx.Query(ctx, `
		SELECT channel, floor(extract(epoch FROM started_at))::bigint, count(*)
		FROM delivery_attempts
		WHERE campaign_id = $1 AND started_at >= $2 AND started_at < $3
		GROUP BY 1, 2`, s.CampaignID, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ch string
		var second int64
		var n int
		if err := rows.Scan(&ch, &second, &n); err != nil {
			return err
		}
		i := second - from.Unix()
		if c := s.Channels[content.Channel(ch)]; c != nil && i >= 0 && i < WindowSeconds {
			c.PerSecond[i] = n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !s.Final() {
		for _, c := range s.Channels {
			c.Rate = c.PerSecond[WindowSeconds-1]
		}
	}
	return nil
}

// readIssues lists the newest calls that did not send a message. The retry
// delay is the difference of two times from the worker's clock.
func (s *Snapshot) readIssues(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `
		SELECT finished_at, channel, http_status, outcome, next_attempt_at, attempt, coalesce(error, '')
		FROM delivery_attempts
		WHERE campaign_id = $1 AND outcome <> 'sent'
		ORDER BY finished_at DESC, id DESC
		LIMIT $2`, s.CampaignID, MaxIssues)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var is Issue
		var next *time.Time
		if err := rows.Scan(&is.At, &is.Channel, &is.Code, &is.Outcome, &next, &is.Attempt, &is.Error); err != nil {
			return err
		}
		is.At = is.At.UTC()
		is.Kind = kind(is.Code)
		if next != nil {
			ms := next.Sub(is.At).Milliseconds()
			is.RetryInMS = &ms
		}
		s.RecentIssues = append(s.RecentIssues, is)
	}
	return rows.Err()
}

// kind classifies a failed call the way the retry policy treats it (D6).
func kind(code *int) string {
	switch {
	case code == nil:
		return "no_answer"
	case *code == 429:
		return "throttled"
	case *code >= 400 && *code < 500:
		return "rejected"
	default:
		return "provider_error"
	}
}
