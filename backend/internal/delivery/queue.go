// Package delivery sends the messages an approval froze: it claims due
// deliveries from Postgres, rate-limits them per channel, calls the
// provider and records each call and its consequence (DECISIONS.md D3–D6,
// D24).
package delivery

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
)

// Queue is the deliveries table used as a queue (D3).
type Queue struct {
	pool *pgxpool.Pool
	// Lease is how long a claim lasts. A worker that dies leaves its rows in
	// sending; once the lease runs out another claim takes them back.
	Lease time.Duration
}

func NewQueue(pool *pgxpool.Pool, lease time.Duration) *Queue {
	return &Queue{pool: pool, Lease: lease}
}

// Job is a claimed delivery.
type Job struct {
	ID         int64
	CampaignID int64
	Channel    content.Channel
	Key        string // Idempotency-Key
	To         string
	Text       string // SMS text, or the email's plain-text body
	Subject    string
	// Attempt is this claim's attempt number. Finish only writes while the
	// row still carries it, which fences off a claim whose lease ran out.
	Attempt   int
	Throttled int // earlier calls answered 429
}

// Claim takes up to n due deliveries of a channel: queued or retrying rows
// whose time has come, and rows whose lease ran out (their worker died).
// FOR UPDATE SKIP LOCKED lets concurrent claimers take disjoint rows without
// waiting for each other. Each claimed row is leased and counts one more
// attempt, so a call cut short by a crash still counts.
func (q *Queue) Claim(ctx context.Context, ch content.Channel, n int) ([]Job, error) {
	rows, err := q.pool.Query(ctx, `
		WITH due AS (
			SELECT id FROM deliveries
			WHERE channel = $1
			  AND ((status IN ('queued', 'retrying') AND next_attempt_at <= now())
			    OR (status = 'sending' AND locked_until < now()))
			ORDER BY next_attempt_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED)
		UPDATE deliveries d
		SET status = 'sending', locked_until = now() + $3::bigint * interval '1 millisecond',
		    attempts = d.attempts + 1, updated_at = now()
		FROM due WHERE d.id = due.id
		RETURNING d.id, d.campaign_id, d.idempotency_key::text, d.recipient,
		          coalesce(d.text, d.body), coalesce(d.subject, ''), d.attempts, d.throttled`,
		string(ch), n, q.Lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		j := Job{Channel: ch}
		if err := rows.Scan(&j.ID, &j.CampaignID, &j.Key, &j.To, &j.Text, &j.Subject, &j.Attempt, &j.Throttled); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// Call is one provider call as the attempt log records it.
type Call struct {
	Result   Result
	Started  time.Time // when the limiter let the call go
	Finished time.Time
}

var outcomes = map[string]string{StatusSent: "sent", StatusRetrying: "retry", StatusFailed: "failed"}

// Finish records a call and its consequence in one transaction: the attempt
// row, and the delivery's new state. It reports false, and writes nothing,
// when the row no longer carries the job's attempt: the lease ran out and
// another claim took the row. That stale call did no harm, because the
// provider deduplicates by key (D5).
func (q *Queue) Finish(ctx context.Context, j Job, c Call, d Decision) (bool, error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) // a no-op after Commit

	throttled := 0
	if c.Result.Kind == Throttled {
		throttled = 1
	}
	var lastError, messageID *string
	if c.Result.Error != "" {
		lastError = &c.Result.Error
	}
	if c.Result.MessageID != "" {
		messageID = &c.Result.MessageID
	}
	// The retry time is computed from the call's own clock, like started_at
	// and finished_at, so the log's "retrying in 2s" is exact even when the
	// database's clock is a few milliseconds off (it is, under Docker on a
	// Mac). Claiming compares it with the database's now(); such a skew only
	// moves the retry by those milliseconds.
	var nextAttempt *time.Time
	if d.Status == StatusRetrying {
		at := c.Finished.Add(d.Delay)
		nextAttempt = &at
	}
	tag, err := tx.Exec(ctx, `
		UPDATE deliveries
		SET status = $3, locked_until = NULL, throttled = throttled + $4,
		    next_attempt_at = coalesce($5, next_attempt_at),
		    last_error = coalesce($6, last_error),
		    provider_message_id = coalesce($7, provider_message_id),
		    sent_at = CASE WHEN $3 = 'sent' THEN $8::timestamptz END,
		    updated_at = now()
		WHERE id = $1 AND status = 'sending' AND attempts = $2`,
		j.ID, j.Attempt, d.Status, throttled, nextAttempt, lastError, messageID, c.Finished)
	if err == nil && tag.RowsAffected() == 0 {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var status *int
	if c.Result.Status != 0 {
		status = &c.Result.Status
	}
	var retryAfter *int64
	if c.Result.RetryAfter > 0 {
		ms := c.Result.RetryAfter.Milliseconds()
		retryAfter = &ms
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO delivery_attempts (delivery_id, campaign_id, channel, attempt, started_at, finished_at,
		                               http_status, outcome, error, retry_after, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::bigint * interval '1 millisecond', $11)`,
		j.ID, j.CampaignID, string(j.Channel), j.Attempt, c.Started, c.Finished,
		status, outcomes[d.Status], lastError, retryAfter, nextAttempt)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// Complete finishes every sending campaign whose deliveries are all final:
// completed when at least one message was sent, failed when none was (D24
// K4; one failed message never fails a campaign). It runs periodically, so
// a crash between a delivery's last call and this check only delays it, and
// the conditional update makes concurrent runs harmless. It returns the
// campaigns it finished.
func (q *Queue) Complete(ctx context.Context) (map[int64]string, error) {
	rows, err := q.pool.Query(ctx, `
		UPDATE campaigns c
		SET status = CASE WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.campaign_id = c.id AND d.status = 'sent')
		                  THEN 'completed' ELSE 'failed' END,
		    finished_at = now(), updated_at = now()
		WHERE c.status = 'sending'
		  AND NOT EXISTS (SELECT 1 FROM deliveries d
		                  WHERE d.campaign_id = c.id AND d.status NOT IN ('sent', 'failed'))
		RETURNING c.id, c.status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := map[int64]string{}
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		done[id] = status
	}
	return done, rows.Err()
}
