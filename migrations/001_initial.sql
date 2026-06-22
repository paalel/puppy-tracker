PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS puppy_state (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    phase            TEXT     NOT NULL DEFAULT 'SLEEPING',
    phase_started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO puppy_state (id, phase, phase_started_at)
VALUES (1, 'SLEEPING', CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS sessions (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    date     TEXT     NOT NULL,
    woke_at  DATETIME NOT NULL,
    slept_at DATETIME
);

CREATE TABLE IF NOT EXISTS meals (
    date       TEXT NOT NULL,
    meal_type  TEXT NOT NULL,
    amount     TEXT NOT NULL DEFAULT 'nothing',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (date, meal_type)
);

CREATE TABLE IF NOT EXISTS config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO config (key, value) VALUES ('puppy_name', 'Nova');
INSERT OR IGNORE INTO config (key, value) VALUES ('awake_minutes', '40');
INSERT OR IGNORE INTO config (key, value) VALUES ('nap_minutes', '90');

CREATE TABLE IF NOT EXISTS routine_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    position   INTEGER NOT NULL,
    label      TEXT    NOT NULL,
    activities TEXT    NOT NULL DEFAULT ''
);
