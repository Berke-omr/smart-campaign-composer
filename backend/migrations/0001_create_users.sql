-- +goose Up

-- Fake customer profiles. Behaviour is stored as derived attributes on the
-- profile (like a CDP profile) rather than as raw events; see DECISIONS.md D14.
CREATE TABLE users (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    first_name       TEXT          NOT NULL CHECK (first_name <> ''),
    last_name        TEXT          NOT NULL CHECK (last_name <> ''),
    email            TEXT          UNIQUE CHECK (email LIKE '_%@_%'),
    phone            TEXT          CHECK (phone ~ '^\+905[0-9]{9}$'),
    city             TEXT          NOT NULL CHECK (city <> ''),
    language         TEXT          NOT NULL DEFAULT 'tr' CHECK (language IN ('tr', 'en')),
    sms_opt_in       BOOLEAN       NOT NULL DEFAULT FALSE,
    email_opt_in     BOOLEAN       NOT NULL DEFAULT FALSE,
    last_cart_add_at TIMESTAMPTZ,
    last_purchase_at TIMESTAMPTZ,
    order_count      INTEGER       NOT NULL DEFAULT 0 CHECK (order_count >= 0),
    total_spent      NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (total_spent >= 0),
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),

    -- Consent is meaningless without a way to reach the user on that channel.
    CONSTRAINT users_sms_opt_in_needs_phone
        CHECK (NOT sms_opt_in OR phone IS NOT NULL),
    CONSTRAINT users_email_opt_in_needs_email
        CHECK (NOT email_opt_in OR email IS NOT NULL),

    -- Purchase attributes must agree with each other.
    CONSTRAINT users_purchase_consistency
        CHECK ((order_count = 0) = (last_purchase_at IS NULL)),
    CONSTRAINT users_spent_consistency
        CHECK ((order_count = 0) = (total_spent = 0)),

    -- A profile cannot have activity before it was created.
    CONSTRAINT users_created_before_activity
        CHECK (created_at <= COALESCE(last_cart_add_at, created_at)
           AND created_at <= COALESCE(last_purchase_at, created_at))
);

-- +goose Down
DROP TABLE users;
