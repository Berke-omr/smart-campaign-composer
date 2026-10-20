-- +goose Up

-- Approval and sending (P4). DECISIONS.md D3, D5, D6 and D24.
ALTER TABLE campaigns
    ADD COLUMN approved_at TIMESTAMPTZ, -- when sending started ("Started 14:32:05")
    ADD COLUMN finished_at TIMESTAMPTZ, -- when every message became final
    ADD CONSTRAINT campaigns_approved CHECK ((status IN ('draft', 'ready')) = (approved_at IS NULL)),
    ADD CONSTRAINT campaigns_finished CHECK ((status IN ('completed', 'failed')) = (finished_at IS NOT NULL));

-- One message to one person on one channel: both the queue and the ledger
-- (D3). Rows are written at approval, in the same transaction that moves the
-- campaign to sending, with the content already rendered for the recipient.
CREATE TABLE deliveries (
    id                  BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Sent as the Idempotency-Key header. A UUID, not the id: ids restart
    -- after a reseed while a provider still remembers the keys it has seen.
    idempotency_key     UUID        NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    campaign_id         BIGINT      NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    user_id             BIGINT      NOT NULL REFERENCES users (id),
    channel             TEXT        NOT NULL CHECK (channel IN ('sms', 'email')),
    recipient           TEXT        NOT NULL CHECK (recipient <> ''), -- phone or email at approval
    text                TEXT,       -- SMS, rendered
    subject             TEXT,       -- email, rendered
    body                TEXT,       -- email, rendered plain text; wrapped in HTML when sent
    status              TEXT        NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued', 'sending', 'sent', 'retrying', 'failed')),
    -- Provider calls started. Incremented when a worker claims the row, so a
    -- call cut short by a crash still counts; it also fences a stale claim.
    attempts            INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    -- How many of those calls were answered 429; they have their own budget.
    throttled           INTEGER     NOT NULL DEFAULT 0 CHECK (throttled >= 0),
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until        TIMESTAMPTZ, -- lease of the worker that claimed the row
    last_error          TEXT,
    provider_message_id TEXT,
    sent_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- "The same message never reaches the same person twice", our side (D5).
    UNIQUE (campaign_id, user_id, channel),
    -- An SMS has text; an email has a subject and a body. The limits hold for
    -- every rendered message, not only for the template (D15).
    CONSTRAINT deliveries_shape CHECK (
        (channel = 'sms'   AND text IS NOT NULL AND subject IS NULL AND body IS NULL
                           AND char_length(text) <= 160) OR
        (channel = 'email' AND text IS NULL AND subject IS NOT NULL AND body IS NOT NULL
                           AND char_length(subject) <= 80 AND subject !~ '[\r\n]')
    ),
    CONSTRAINT deliveries_throttled_within_attempts CHECK (throttled <= attempts),
    CONSTRAINT deliveries_lease CHECK ((status = 'sending') = (locked_until IS NOT NULL)),
    CONSTRAINT deliveries_sent CHECK ((status = 'sent') = (sent_at IS NOT NULL))
);

-- The worker's two claim paths: due rows, and rows whose lease ran out.
CREATE INDEX deliveries_due_idx ON deliveries (channel, next_attempt_at) WHERE status IN ('queued', 'retrying');
CREATE INDEX deliveries_lease_idx ON deliveries (channel, locked_until) WHERE status = 'sending';
-- Per-campaign counts (dashboard snapshots, D7) and the completion check.
CREATE INDEX deliveries_campaign_idx ON deliveries (campaign_id, channel, status);

-- One row per provider call. It feeds the dashboard's "Recent issues" and the
-- sends-per-second chart (D22), and is where the rate limit is measured.
CREATE TABLE delivery_attempts (
    id              BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id     BIGINT      NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
    campaign_id     BIGINT      NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    channel         TEXT        NOT NULL CHECK (channel IN ('sms', 'email')),
    attempt         INTEGER     NOT NULL CHECK (attempt >= 1),
    started_at      TIMESTAMPTZ NOT NULL, -- taken right after the rate limiter let the call go
    finished_at     TIMESTAMPTZ NOT NULL,
    http_status     INTEGER,              -- NULL on a timeout or network error
    outcome         TEXT        NOT NULL CHECK (outcome IN ('sent', 'retry', 'failed')),
    error           TEXT,
    retry_after     INTERVAL,             -- the provider's Retry-After on a 429
    next_attempt_at TIMESTAMPTZ,          -- when the retry is due ("retrying in 2s")
    UNIQUE (delivery_id, attempt),
    CONSTRAINT delivery_attempts_retry_due CHECK ((outcome = 'retry') = (next_attempt_at IS NOT NULL))
);

CREATE INDEX delivery_attempts_campaign_idx ON delivery_attempts (campaign_id, started_at DESC);

-- +goose Down
DROP TABLE delivery_attempts;
DROP TABLE deliveries;
ALTER TABLE campaigns
    DROP CONSTRAINT campaigns_finished,
    DROP CONSTRAINT campaigns_approved,
    DROP COLUMN finished_at,
    DROP COLUMN approved_at;
