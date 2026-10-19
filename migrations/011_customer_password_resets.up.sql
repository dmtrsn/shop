-- Self-service password recovery. One row per customer is its single active
-- reset token: a new request replaces it, and a successful reset, a disable,
-- an enable or an operator password change deletes it. token_hash is a
-- SHA-256 digest of the emailed token; the raw token is never stored. Users
-- are disabled, never deleted, but a deleted user takes its token along.
-- Earlier binaries never read this table.
CREATE TABLE customer_password_resets (
    user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash text NOT NULL
        CONSTRAINT customer_password_resets_token_digest CHECK (token_hash ~ '^[A-Za-z0-9_-]{43}$'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CONSTRAINT customer_password_resets_lifetime CHECK (expires_at > created_at)
);
CREATE UNIQUE INDEX customer_password_resets_token_hash_uidx ON customer_password_resets (token_hash);
CREATE INDEX customer_password_resets_expiry_idx ON customer_password_resets (expires_at);
