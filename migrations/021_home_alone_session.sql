-- Home-alone becomes a session of its own kind (alone = 1) rather than a flag
-- toggled over an interval, so the old pen_sessions table is no longer used.
DROP TABLE IF EXISTS pen_sessions;

-- Quality fields tracked only for home-alone sessions. Normal sessions leave
-- these empty. The note reuses the existing `comment` column.
ALTER TABLE sessions ADD COLUMN alone_slept TEXT;
ALTER TABLE sessions ADD COLUMN alone_behaviour TEXT;
ALTER TABLE sessions ADD COLUMN alone_location TEXT;
ALTER TABLE sessions ADD COLUMN alone_destroyed INTEGER NOT NULL DEFAULT 0;
