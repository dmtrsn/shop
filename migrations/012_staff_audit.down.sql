-- Destroys the audit history; for development databases only.
DROP TABLE staff_audit_events;
DROP FUNCTION staff_audit_events_append_only();
