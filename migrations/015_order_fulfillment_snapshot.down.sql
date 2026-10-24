-- Discards every recorded order-time contact; for development databases only.
ALTER TABLE orders DROP CONSTRAINT orders_customer_snapshot_complete,
    DROP COLUMN customer_phone, DROP COLUMN customer_email, DROP COLUMN customer_name;
