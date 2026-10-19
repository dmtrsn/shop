-- Re-lists every archived product; for development databases only.
ALTER TABLE products DROP COLUMN archived_at, DROP COLUMN revision;
