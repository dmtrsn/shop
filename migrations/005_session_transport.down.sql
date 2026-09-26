-- Destroys the bearer/cookie binding: cookie sessions would become usable as
-- bearer tokens by anyone holding the raw value. This is not an application
-- rollback; never run it against a production database.
ALTER TABLE sessions DROP COLUMN transport;
