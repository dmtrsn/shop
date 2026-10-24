-- Staff order management (ADR 016). revision is the optimistic-concurrency
-- version of an order: every staff status change increments it, so a change
-- based on an older read is refused. Existing orders start at 1; checkout
-- keeps inserting without it. Statuses stay free text with a stored label, so
-- earlier binaries show the statuses staff set (transit, cancelled) as they
-- are and ignore the column.
ALTER TABLE orders
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CONSTRAINT orders_revision_positive CHECK (revision > 0);
