-- Training tracker for the simplified app. Skills are configured in settings;
-- reps are logged (with a quality mark) against a skill from the Today card.

CREATE TABLE IF NOT EXISTS training_skills (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    instruction TEXT NOT NULL DEFAULT '',
    target      INTEGER NOT NULL DEFAULT 1,
    period      TEXT NOT NULL DEFAULT 'day',  -- 'day' | 'week'
    position    INTEGER NOT NULL DEFAULT 0,
    deleted_at  DATETIME
);

CREATE TABLE IF NOT EXISTS training_reps (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    day        TEXT NOT NULL,                 -- rollover-aware local date
    at         DATETIME NOT NULL,
    skill_id   INTEGER NOT NULL REFERENCES training_skills(id),
    quality    TEXT NOT NULL,                 -- 'bad' | 'okay' | 'good' | 'excellent'
    deleted_at DATETIME
);
