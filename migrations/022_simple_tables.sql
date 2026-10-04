-- Tables for the simplified grown-dog app. Kept separate from the classic
-- sessions table so the two versions don't interfere. Home-alone is NOT here —
-- it reuses the sessions alone rows (same data as classic).
--
-- `day` is the rollover-aware local date (YYYY-MM-DD), stamped at insert the same
-- way sessions.date is, so "today" filtering is a plain `day = ?` and avoids the
-- pure-Go driver's unreliable 'localtime' modifier.

CREATE TABLE IF NOT EXISTS poops (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    day        TEXT NOT NULL,
    at         DATETIME NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'poop', -- 'poop' | 'accident'
    deleted_at DATETIME
);

CREATE TABLE IF NOT EXISTS naps (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    day        TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    ended_at   DATETIME,                      -- NULL while running
    deleted_at DATETIME
);

CREATE TABLE IF NOT EXISTS outings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    day        TEXT NOT NULL,
    at         DATETIME NOT NULL,
    where_json TEXT NOT NULL DEFAULT '[]',    -- JSON array of place tags
    what_json  TEXT NOT NULL DEFAULT '[]',    -- JSON array of activity tags
    deleted_at DATETIME
);

-- Soft-delete for home-alone sessions in the simplified app. Only the simplified
-- alone queries filter on it; classic rendering is left as is.
ALTER TABLE sessions ADD COLUMN deleted_at DATETIME;
