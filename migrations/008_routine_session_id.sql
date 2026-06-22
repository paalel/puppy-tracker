ALTER TABLE sessions ADD COLUMN routine_session_id INTEGER REFERENCES routine_sessions(id);

CREATE TEMPORARY TABLE _pos AS
SELECT s.id AS sid,
       (SELECT COUNT(*) FROM sessions prev WHERE prev.date = s.date AND prev.id < s.id) AS idx
FROM sessions s;

UPDATE sessions SET routine_session_id = (
    SELECT rs.id FROM routine_sessions rs
    INNER JOIN _pos ON _pos.sid = sessions.id AND rs.position = _pos.idx + 1
) WHERE routine_session_id IS NULL;

DROP TABLE _pos;
