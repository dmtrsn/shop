-- Staff product management (ADR 013). revision is the optimistic-concurrency
-- version of a product row: every staff product mutation and every checkout
-- stock decrement increments it, so an edit based on an older read cannot
-- overwrite a newer one. Existing rows start at 1. archived_at withdraws a
-- product from sale without deleting it: carts, favorites and order history
-- keep their rows. Earlier binaries ignore both columns and would show
-- archived products as active again (see ADR 013, Rollback).
ALTER TABLE products
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CONSTRAINT products_revision_positive CHECK (revision > 0),
    ADD COLUMN archived_at timestamptz;
