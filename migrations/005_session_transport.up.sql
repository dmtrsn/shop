-- Binds every session to the transport that issued it. Existing rows were all
-- issued as bearer tokens. The constant default is metadata-only (no table
-- rewrite) and keeps older binaries, which insert without this column, working
-- after an application rollback.
ALTER TABLE sessions
    ADD COLUMN transport text NOT NULL DEFAULT 'bearer'
    CONSTRAINT sessions_transport_check CHECK (transport IN ('bearer', 'cookie'));
