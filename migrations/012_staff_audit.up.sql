-- One row per staff business mutation, written in the mutation's own
-- transaction (ADR 012), so a change and its row commit or roll back
-- together. Rows are never updated or deleted. The actor references
-- staff_users and restricts its deletion (staff are disabled, never deleted);
-- the target is a typed reference, not a foreign key, so history outlives the
-- resource. request_id correlates with logs; the client may choose it, so it
-- is neither unique nor proof of anything. created_at is the database clock.
-- There is deliberately no free-form details column yet. Earlier binaries
-- never read this table.
CREATE TABLE staff_audit_events (
    id text PRIMARY KEY CONSTRAINT staff_audit_events_id_format CHECK (id ~ '^aud_[0-9a-f]{32}$'),
    staff_user_id text NOT NULL REFERENCES staff_users(id) ON DELETE RESTRICT,
    action text NOT NULL
        CONSTRAINT staff_audit_events_action_format CHECK (action ~ '^[a-z][a-z0-9_]{0,31}\.[a-z][a-z0-9_]{0,31}$'),
    entity_type text NOT NULL
        CONSTRAINT staff_audit_events_entity_type_format CHECK (entity_type ~ '^[a-z][a-z0-9_]{0,31}$'),
    entity_id text NOT NULL
        CONSTRAINT staff_audit_events_entity_id_format CHECK (entity_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    request_id text
        CONSTRAINT staff_audit_events_request_id_format CHECK (request_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);
-- "What did this staff member do" and "history of this resource".
CREATE INDEX staff_audit_events_staff_user_idx ON staff_audit_events (staff_user_id, created_at);
CREATE INDEX staff_audit_events_entity_idx ON staff_audit_events (entity_type, entity_id, created_at);

-- Append-only against accidental application SQL. The application role owns
-- the table and could still drop these triggers; this is not tamper evidence.
CREATE FUNCTION staff_audit_events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'staff_audit_events is append-only' USING ERRCODE = 'restrict_violation';
END
$$;
CREATE TRIGGER staff_audit_events_no_change BEFORE UPDATE OR DELETE ON staff_audit_events
    FOR EACH ROW EXECUTE FUNCTION staff_audit_events_append_only();
CREATE TRIGGER staff_audit_events_no_truncate BEFORE TRUNCATE ON staff_audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION staff_audit_events_append_only();
