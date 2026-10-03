-- Optional checkout idempotency. The key and the server-computed request digest
-- are stored on the committed order itself, so they exist exactly when the
-- order does: failed attempts leave nothing behind. Keys are unique per user
-- only. Existing orders stay NULL/NULL; adding nullable columns is
-- metadata-only and older binaries keep inserting without them.
ALTER TABLE orders
    ADD COLUMN idempotency_key text,
    ADD COLUMN request_digest text;
ALTER TABLE orders
    ADD CONSTRAINT orders_idempotency_pair_chk CHECK ((idempotency_key IS NULL) = (request_digest IS NULL));
CREATE UNIQUE INDEX orders_user_idempotency_key_idx ON orders(user_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
