-- +goose Up

-- A campaign from brief to send. P3 creates it as a draft; P4 moves it on
-- (ready -> sending -> completed / failed). DECISIONS.md D22.
CREATE TABLE campaigns (
    id                   BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                 TEXT        NOT NULL CHECK (name <> ''),
    brief                TEXT        NOT NULL CHECK (brief <> ''),
    tone                 TEXT        NOT NULL CHECK (tone IN ('friendly', 'professional', 'playful', 'urgent')),
    channels             TEXT[]      NOT NULL
                         CHECK (cardinality(channels) > 0 AND channels <@ ARRAY['sms', 'email']::TEXT[]),
    status               TEXT        NOT NULL DEFAULT 'draft'
                         CHECK (status IN ('draft', 'ready', 'sending', 'completed', 'failed')),
    -- The validated rule, as accepted by segment.Validate. Never SQL.
    segment_rule         JSONB       NOT NULL,
    unsupported_criteria JSONB       NOT NULL DEFAULT '[]',
    -- Anchor for "in the last N days": counts and longest values are
    -- reproducible for as long as the draft lives (DECISIONS.md D9).
    audience_as_of       TIMESTAMPTZ NOT NULL,
    llm_mode             TEXT        NOT NULL CHECK (llm_mode IN ('anthropic', 'mock')),
    llm_model            TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX campaigns_created_at_idx ON campaigns (created_at DESC);

-- The current message of each selected channel. A missing row means the
-- channel has not been drafted yet. Text, subject and body are templates
-- with {{variables}}; they are rendered per recipient at send time (P4).
CREATE TABLE campaign_messages (
    campaign_id BIGINT      NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    channel     TEXT        NOT NULL CHECK (channel IN ('sms', 'email')),
    state       TEXT        NOT NULL CHECK (state IN ('valid', 'needs_manual_edit')),
    text        TEXT,
    subject     TEXT,
    body        TEXT,
    source      TEXT        NOT NULL CHECK (source IN ('generated', 'edited')),
    attempts    INTEGER     NOT NULL CHECK (attempts >= 0),
    problems    JSONB       NOT NULL DEFAULT '[]',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (campaign_id, channel),

    -- An SMS has text; an email has a subject and a body.
    CONSTRAINT campaign_messages_shape CHECK (
        (channel = 'sms'   AND subject IS NULL AND body IS NULL) OR
        (channel = 'email' AND text IS NULL)
    ),
    -- A valid message has its content; one that needs a manual edit may
    -- have none (e.g. the model declined twice).
    CONSTRAINT campaign_messages_valid_has_content CHECK (
        state <> 'valid' OR
        (channel = 'sms'   AND text IS NOT NULL) OR
        (channel = 'email' AND subject IS NOT NULL AND body IS NOT NULL)
    )
);

-- +goose Down
DROP TABLE campaign_messages;
DROP TABLE campaigns;
