-- Removes checkout idempotency records: retries of already committed keyed
-- checkouts would no longer replay. Run only together with an application
-- rollback to a binary that does not send or read these columns.
DROP INDEX IF EXISTS orders_user_idempotency_key_idx;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_idempotency_pair_chk;
ALTER TABLE orders
    DROP COLUMN IF EXISTS request_digest,
    DROP COLUMN IF EXISTS idempotency_key;
