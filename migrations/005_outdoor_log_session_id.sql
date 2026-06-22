DROP TABLE IF EXISTS outdoor_logs;
CREATE TABLE IF NOT EXISTS outdoor_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL REFERENCES sessions(id),
    logged_at TEXT NOT NULL,
    result TEXT NOT NULL
);
