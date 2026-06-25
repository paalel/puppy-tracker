ALTER TABLE sessions ADD COLUMN toilet_pee      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN toilet_poop     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN toilet_accident INTEGER NOT NULL DEFAULT 0;

UPDATE sessions SET toilet_pee      = 1 WHERE toilet IN ('pee',  'both');
UPDATE sessions SET toilet_poop     = 1 WHERE toilet IN ('poop', 'both');
UPDATE sessions SET toilet_accident = 1 WHERE toilet = 'accident';

ALTER TABLE night_toilets ADD COLUMN toilet_pee      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE night_toilets ADD COLUMN toilet_poop     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE night_toilets ADD COLUMN toilet_accident INTEGER NOT NULL DEFAULT 0;

UPDATE night_toilets SET toilet_pee  = 1 WHERE toilet IN ('pee',  'both');
UPDATE night_toilets SET toilet_poop = 1 WHERE toilet IN ('poop', 'both');
