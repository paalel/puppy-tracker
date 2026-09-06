-- "Home alone" mode. An open row in pen_sessions (ended_at IS NULL) means the
-- puppy is currently home alone; sessions logged during that interval get
-- alone = 1. Alone sessions still appear in the schedule/history but are kept
-- out of the statistics, since settling without a human present is different.
ALTER TABLE sessions ADD COLUMN alone INTEGER NOT NULL DEFAULT 0;
