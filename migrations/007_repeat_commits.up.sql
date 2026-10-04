-- Only successful commits are persisted; previews have no lifecycle.
CREATE TABLE repeat_commits (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9_-]{16,128}$'),
    request_digest text NOT NULL CHECK (request_digest ~ '^[a-f0-9]{64}$'),
    source_order_id text NOT NULL,
    receipt jsonb NOT NULL CHECK (
        jsonb_typeof(receipt) = 'object'
        AND receipt ? 'sourceOrderId'
		AND jsonb_typeof(receipt->'sourceOrderId') = 'string'
        AND receipt->>'sourceOrderId' = source_order_id
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repeat_commits_user_key UNIQUE (user_id, idempotency_key)
);
