-- Timestamped markers within an alone (pen) session: when she drifted asleep,
-- woke, got restless, or toileted. Kept separate from the sessions table so
-- unsupervised alone-time never skews the nap/settle/poop statistics.
CREATE TABLE IF NOT EXISTS pen_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    pen_session_id INTEGER NOT NULL REFERENCES pen_sessions(id),
    occurred_at    DATETIME NOT NULL,
    kind           TEXT NOT NULL  -- 'asleep', 'awake', 'restless', 'toilet'
);
