-- Order-time customer contact (ADR 017). Checkout copies the customer's name,
-- email and phone, read from the users row it already holds locked, into the
-- order it creates, so the order keeps whom it was placed for even if the
-- account changes. Delivery address and comment were already stored per
-- order. The three columns are written together or not at all: pre-B5
-- orders (and any order an older replica writes during a rolling deploy)
-- keep NULL, meaning "not recorded", and are never backfilled from today's
-- account. Nothing updates them after insert.
ALTER TABLE orders
    ADD COLUMN customer_name text,
    ADD COLUMN customer_email text,
    ADD COLUMN customer_phone text,
    ADD CONSTRAINT orders_customer_snapshot_complete CHECK (
        (customer_name IS NULL) = (customer_email IS NULL) AND (customer_email IS NULL) = (customer_phone IS NULL));
