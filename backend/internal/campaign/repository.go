// Package campaign is a campaign from brief to send: drafts are created from
// a brief, written per channel, edited, and (in P4) approved and sent.
package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
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
	ErrNotFound = errors.New("campaign not found")
	ErrLocked   = errors.New("campaign content can no longer be changed")
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
	audience_as_of, llm_mode, llm_model, created_at, updated_at`

func scanCampaign(row pgx.Row) (Campaign, error) {
	var c Campaign
	var channels []string
	var rule, unsupported []byte
	err := row.Scan(&c.ID, &c.Name, &c.Brief, &c.Tone, &channels, &c.Status, &rule, &unsupported,
		&c.AsOf, &c.LLMMode, &c.LLMModel, &c.CreatedAt, &c.UpdatedAt)
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

// Get loads a campaign with its messages.
func (r *Repository) Get(ctx context.Context, id int64) (Campaign, error) {
	c, err := scanCampaign(r.pool.QueryRow(ctx, `SELECT `+campaignColumns+` FROM campaigns WHERE id = $1`, id))
	if err != nil {
		return Campaign{}, err
	}
	rows, err := r.pool.Query(ctx, `
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
