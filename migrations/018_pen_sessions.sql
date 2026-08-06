CREATE TABLE IF NOT EXISTS pen_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME NOT NULL,
    ended_at   DATETIME
);
