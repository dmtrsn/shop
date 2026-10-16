-- Staff are a separate principal: their own accounts and sessions, never rows
-- of users or sessions. Accounts are disabled, not deleted, so sessions and
-- future audit rows restrict deletion. Earlier binaries ignore these tables.
CREATE TABLE staff_users (
    id text PRIMARY KEY CONSTRAINT staff_users_id_format CHECK (id ~ '^stf_[0-9a-f]{32}$'),
    email text NOT NULL,
    display_name text NOT NULL,
    password_hash text NOT NULL,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX staff_users_email_lower_uidx ON staff_users (lower(email));

-- token_hash is the SHA-256 digest of the opaque cookie token; the raw token
-- is never stored.
CREATE TABLE staff_sessions (
    token_hash text PRIMARY KEY,
    staff_user_id text NOT NULL REFERENCES staff_users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX staff_sessions_staff_user_idx ON staff_sessions (staff_user_id);
CREATE INDEX staff_sessions_expiry_idx ON staff_sessions (expires_at);
