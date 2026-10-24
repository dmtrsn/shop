-- Re-lists every archived dictionary row; for development databases only.
ALTER TABLE collections DROP COLUMN archived_at, DROP COLUMN revision;
ALTER TABLE brands DROP COLUMN archived_at, DROP COLUMN revision;
ALTER TABLE manufacturers DROP COLUMN archived_at, DROP COLUMN revision;
ALTER TABLE categories DROP COLUMN archived_at, DROP COLUMN revision;
