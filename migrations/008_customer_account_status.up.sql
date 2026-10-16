-- Operator-controlled account disable; NULL means active. A disabled account
-- cannot sign in and its sessions are refused. Adding a nullable column is
-- metadata-only, and earlier binaries, which name their columns, ignore it.
ALTER TABLE users ADD COLUMN disabled_at timestamptz;
