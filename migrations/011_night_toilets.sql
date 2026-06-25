CREATE TABLE IF NOT EXISTS night_toilets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at DATETIME NOT NULL,
    toilet      TEXT NOT NULL  -- 'pee', 'poop', 'both', 'nothing'
);
