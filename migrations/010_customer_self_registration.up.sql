-- Self-registration state. A pending registration is not an account: it has
-- no users row, so it cannot sign in and owns no cart, favorites, orders or
-- sessions. Confirmation moves it into users and deletes it. Earlier binaries
-- never read these tables.
--
-- email is the normalized address and the key, so an address has at most one
-- pending registration and one confirmation token. token_hash is a SHA-256
-- digest of the emailed token; the raw token is never stored. password_hash
-- is the bcrypt hash that becomes users.password_hash on confirmation.
CREATE TABLE customer_registrations (
    email text PRIMARY KEY
        CONSTRAINT customer_registrations_email_normalized CHECK (email = lower(email) AND email <> ''),
    name text NOT NULL,
    phone text NOT NULL DEFAULT '',
    password_hash text NOT NULL
        CONSTRAINT customer_registrations_bcrypt CHECK (password_hash ~ '^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$'),
    token_hash text NOT NULL
        CONSTRAINT customer_registrations_token_digest CHECK (token_hash ~ '^[A-Za-z0-9_-]{43}$'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CONSTRAINT customer_registrations_lifetime CHECK (expires_at > created_at)
);
CREATE UNIQUE INDEX customer_registrations_token_hash_uidx ON customer_registrations (token_hash);
CREATE INDEX customer_registrations_expiry_idx ON customer_registrations (expires_at);

-- Outbound account email per normalized address, shared by every replica:
-- sent_at holds the send times of the current 24-hour window (oldest first),
-- last_sent_at the latest one for the cooldown and for cleanup.
CREATE TABLE customer_email_throttle (
    email text PRIMARY KEY
        CONSTRAINT customer_email_throttle_email_normalized CHECK (email = lower(email) AND email <> ''),
    sent_at timestamptz[] NOT NULL
        CONSTRAINT customer_email_throttle_sent_at_nonempty CHECK (cardinality(sent_at) >= 1),
    last_sent_at timestamptz NOT NULL
);
CREATE INDEX customer_email_throttle_last_sent_idx ON customer_email_throttle (last_sent_at);
