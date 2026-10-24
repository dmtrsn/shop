-- Staff management of the catalog dictionaries (ADR 018). revision is the
-- optimistic-concurrency version of each row (every staff change increments
-- it); archived_at withdraws a row from the storefront and from new product
-- assignments without deleting it. Nothing is ever hard-deleted. Existing rows
-- start at revision 1, active. Earlier binaries ignore both columns and would
-- show archived rows again (see ADR 018, Rollback).
ALTER TABLE categories
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CONSTRAINT categories_revision_positive CHECK (revision > 0),
    ADD COLUMN archived_at timestamptz;
ALTER TABLE manufacturers
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CONSTRAINT manufacturers_revision_positive CHECK (revision > 0),
    ADD COLUMN archived_at timestamptz;
ALTER TABLE brands
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CONSTRAINT brands_revision_positive CHECK (revision > 0),
    ADD COLUMN archived_at timestamptz;
ALTER TABLE collections
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CONSTRAINT collections_revision_positive CHECK (revision > 0),
    ADD COLUMN archived_at timestamptz;
