-- Remove schema that no code references any more.
-- night_toilets (011) and outdoor_log (004/005) were superseded by the
-- toilet_* booleans on sessions. The bare `toilet` TEXT column (006) and
-- `sleep_interrupted` (007) likewise. pen_sessions (018) is intentionally
-- kept because it backs the "Alone" mode.
--
-- DROP COLUMN is not idempotent, but runMigration re-runs every file on each
-- boot and swallows the resulting "no such column" once applied.
DROP TABLE IF EXISTS night_toilets;
DROP TABLE IF EXISTS outdoor_log;
ALTER TABLE sessions DROP COLUMN toilet;
ALTER TABLE sessions DROP COLUMN sleep_interrupted;
