// Package campaign is a campaign from brief to send: drafts are created from
// a brief, written per channel, edited, and (in P4) approved and sent.
package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// Status is a campaign's lifecycle state (DECISIONS.md D22). The UI shows
// them as Draft, Ready, Sending, Completed and Failed.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusReady     Status = "ready"
	StatusSending   Status = "sending"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Editable reports whether the content can still change. After approval it
// is locked (assumption A12).
func (s Status) Editable() bool { return s == StatusDraft || s == StatusReady }

// Message sources.
const (
	SourceGenerated = "generated"
	SourceEdited    = "edited"
)

var (
	ErrNotFound     = errors.New("campaign not found")
	ErrLocked       = errors.New("campaign content can no longer be changed")
	ErrNotReady     = errors.New("campaign is not ready to send: every selected channel needs a valid message")
	ErrNoRecipients = errors.New("no one in this audience can be reached on the selected channels")
)

// Campaign is a stored campaign.
type Campaign struct {
	ID                  int64
	Name                string
	Brief               string
	Tone                llm.Tone
	Channels            []content.Channel
	Status              Status
	Rule                json.RawMessage
	UnsupportedCriteria []string
	AsOf                time.Time
	LLMMode             string
	LLMModel            *string
	ApprovedAt          *time.Time
	FinishedAt          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	// Messages holds the stored message of each drafted channel.
	Messages map[content.Channel]StoredMessage
}

// StoredMessage is a channel's current message.
type StoredMessage struct {
	State     llm.State
	Message   content.Message
	Source    string
	Attempts  int
	Problems  []string
	UpdatedAt time.Time
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a draft campaign and fills in its id and timestamps.
func (r *Repository) Create(ctx context.Context, c *Campaign) error {
	unsupported, err := json.Marshal(c.UnsupportedCriteria)
	if err != nil {
		return err
	}
	return r.pool.QueryRow(ctx, `
		INSERT INTO campaigns (name, brief, tone, channels, status, segment_rule, unsupported_criteria,
		                       audience_as_of, llm_mode, llm_model)
		VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $8, $9)
		RETURNING id, status, created_at, updated_at`,
		c.Name, c.Brief, string(c.Tone), channelStrings(c.Channels), []byte(c.Rule), unsupported,
		c.AsOf, c.LLMMode, c.LLMModel,
	).Scan(&c.ID, &c.Status, &c.CreatedAt, &c.UpdatedAt)
}

const campaignColumns = `id, name, brief, tone, channels, status, segment_rule, unsupported_criteria,
	audience_as_of, llm_mode, llm_model, approved_at, finished_at, created_at, updated_at`

func scanCampaign(row pgx.Row) (Campaign, error) {
	var c Campaign
	var channels []string
	var rule, unsupported []byte
	err := row.Scan(&c.ID, &c.Name, &c.Brief, &c.Tone, &channels, &c.Status, &rule, &unsupported,
		&c.AsOf, &c.LLMMode, &c.LLMModel, &c.ApprovedAt, &c.FinishedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrNotFound
	}
	if err != nil {
		return Campaign{}, err
	}
	c.Rule = rule
	if err := json.Unmarshal(unsupported, &c.UnsupportedCriteria); err != nil {
		return Campaign{}, err
	}
	for _, ch := range channels {
		c.Channels = append(c.Channels, content.Channel(ch))
	}
	return c, nil
}

// querier is what reading a campaign needs; a pool and a transaction both
// have it.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Get loads a campaign with its messages.
func (r *Repository) Get(ctx context.Context, id int64) (Campaign, error) {
	return get(ctx, r.pool, id, false)
}

// get loads a campaign with its messages; lock takes the campaign row lock
// first (inside a transaction).
func get(ctx context.Context, q querier, id int64, lock bool) (Campaign, error) {
	sql := `SELECT ` + campaignColumns + ` FROM campaigns WHERE id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	c, err := scanCampaign(q.QueryRow(ctx, sql, id))
	if err != nil {
		return Campaign{}, err
	}
	rows, err := q.Query(ctx, `
		SELECT channel, state, coalesce(text, ''), coalesce(subject, ''), coalesce(body, ''),
		       source, attempts, problems, updated_at
		FROM campaign_messages WHERE campaign_id = $1`, id)
	if err != nil {
		return Campaign{}, err
	}
	defer rows.Close()
	c.Messages = map[content.Channel]StoredMessage{}
	for rows.Next() {
		var ch string
		var m StoredMessage
		var problems []byte
		if err := rows.Scan(&ch, &m.State, &m.Message.Text, &m.Message.Subject, &m.Message.Body,
			&m.Source, &m.Attempts, &problems, &m.UpdatedAt); err != nil {
			return Campaign{}, err
		}
		if err := json.Unmarshal(problems, &m.Problems); err != nil {
			return Campaign{}, err
		}
		c.Messages[content.Channel(ch)] = m
	}
	return c, rows.Err()
}

// List returns the most recent campaigns, without their messages.
func (r *Repository) List(ctx context.Context, limit int) ([]Campaign, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+campaignColumns+` FROM campaigns ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Campaign{}
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveMessage stores a channel's message and recomputes the campaign's
// status in the same transaction: ready when every selected channel has a
// valid message, draft otherwise (DECISIONS.md K7). It returns the new
// status, or ErrLocked once the campaign has left draft/ready.
func (r *Repository) SaveMessage(ctx context.Context, id int64, ch content.Channel, m StoredMessage) (Status, error) {
	problems, err := json.Marshal(m.Problems)
	if err != nil {
		return "", err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) // a no-op after Commit

	var status Status
	var channels []string
	// The row lock serializes saves for one campaign (SMS and email finish
	// concurrently) and orders them with P4's approval.
	err = tx.QueryRow(ctx, `SELECT status, channels FROM campaigns WHERE id = $1 FOR UPDATE`, id).Scan(&status, &channels)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !status.Editable() {
		return status, ErrLocked
	}

	text, subject, body := nullable(m.Message.Text), nullable(m.Message.Subject), nullable(m.Message.Body)
	if ch == content.SMS {
		subject, body = nil, nil
	} else {
		text = nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO campaign_messages (campaign_id, channel, state, text, subject, body, source, attempts, problems, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (campaign_id, channel) DO UPDATE
		SET state = EXCLUDED.state, text = EXCLUDED.text, subject = EXCLUDED.subject, body = EXCLUDED.body,
		    source = EXCLUDED.source, attempts = EXCLUDED.attempts, problems = EXCLUDED.problems, updated_at = now()`,
		id, string(ch), string(m.State), text, subject, body, m.Source, m.Attempts, problems)
	if err != nil {
		return "", err
	}

	err = tx.QueryRow(ctx, `
		UPDATE campaigns SET updated_at = now(), status = CASE
		    WHEN (SELECT count(*) FROM campaign_messages
		          WHERE campaign_id = $1 AND state = 'valid' AND channel = ANY(campaigns.channels)) = cardinality(campaigns.channels)
		    THEN 'ready' ELSE 'draft' END
		WHERE id = $1
		RETURNING status`, id).Scan(&status)
	if err != nil {
		return "", err
	}
	return status, tx.Commit(ctx)
}

// NewDelivery is one message frozen at approval, rendered for its recipient.
type NewDelivery struct {
	UserID    int64
	Channel   content.Channel
	Recipient string
	Message   content.Message
}

// Freeze builds the deliveries of a ready campaign. Approve calls it inside
// its transaction, after the campaign row is locked; q reads the audience
// in that transaction.
type Freeze func(ctx context.Context, c Campaign, q segment.Querier) ([]NewDelivery, error)

// Approval is what an approval froze.
type Approval struct {
	CampaignID int64          `json:"campaign_id"`
	Status     Status         `json:"status"`
	ApprovedAt time.Time      `json:"approved_at"`
	Deliveries DeliveryCounts `json:"deliveries"`
}

// DeliveryCounts are the messages per channel; one person reachable on both
// channels gets two.
type DeliveryCounts struct {
	SMS   int64 `json:"sms"`
	Email int64 `json:"email"`
	Total int64 `json:"total"`
}

var deliveryColumns = []string{"campaign_id", "user_id", "channel", "recipient", "text", "subject", "body"}

// Approve moves a ready campaign to sending and inserts its deliveries in
// the same transaction, so there is no "approved but never queued" state
// (DECISIONS.md D3). The campaign row is locked first, so approvals and
// edits of one campaign run one after the other: a second approval finds the
// campaign already sending and returns the same result without writing
// anything (D5).
func (r *Repository) Approve(ctx context.Context, id int64, freeze Freeze) (Approval, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Approval{}, err
	}
	defer tx.Rollback(ctx) // a no-op after Commit

	c, err := get(ctx, tx, id, true)
	if err != nil {
		return Approval{}, err
	}
	switch c.Status {
	case StatusSending, StatusCompleted, StatusFailed:
		a, err := approval(ctx, tx, id)
		if err != nil {
			return Approval{}, err
		}
		return a, tx.Commit(ctx)
	case StatusDraft:
		return Approval{}, ErrNotReady
	}

	ds, err := freeze(ctx, c, tx)
	if err != nil {
		return Approval{}, err
	}
	if len(ds) == 0 {
		return Approval{}, ErrNoRecipients
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"deliveries"}, deliveryColumns, pgx.CopyFromSlice(len(ds), func(i int) ([]any, error) {
		d := ds[i]
		text, subject, body := nullable(d.Message.Text), nullable(d.Message.Subject), nullable(d.Message.Body)
		if d.Channel == content.SMS {
			subject, body = nil, nil
		} else {
			text = nil
		}
		return []any{id, d.UserID, string(d.Channel), d.Recipient, text, subject, body}, nil
	}))
	if err != nil {
		return Approval{}, fmt.Errorf("insert deliveries: %w", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE campaigns SET status = 'sending', approved_at = now(), updated_at = now()
	                          WHERE id = $1 AND status = 'ready'`, id)
	if err != nil {
		return Approval{}, err
	}
	if tag.RowsAffected() != 1 {
		return Approval{}, fmt.Errorf("campaign %d left ready during its approval", id)
	}
	a, err := approval(ctx, tx, id)
	if err != nil {
		return Approval{}, err
	}
	return a, tx.Commit(ctx)
}

// approval reads an approved campaign's status, start time and deliveries.
func approval(ctx context.Context, q querier, id int64) (Approval, error) {
	a := Approval{CampaignID: id}
	err := q.QueryRow(ctx, `
		SELECT c.status, c.approved_at,
		       count(d.id) FILTER (WHERE d.channel = 'sms'), count(d.id) FILTER (WHERE d.channel = 'email')
		FROM campaigns c LEFT JOIN deliveries d ON d.campaign_id = c.id
		WHERE c.id = $1
		GROUP BY c.id`, id).Scan(&a.Status, &a.ApprovedAt, &a.Deliveries.SMS, &a.Deliveries.Email)
	a.Deliveries.Total = a.Deliveries.SMS + a.Deliveries.Email
	return a, err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func channelStrings(chs []content.Channel) []string {
	out := make([]string, len(chs))
	for i, ch := range chs {
		out[i] = string(ch)
	}
	return out
}
