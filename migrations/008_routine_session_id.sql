ALTER TABLE sessions ADD COLUMN routine_session_id INTEGER REFERENCES routine_sessions(id);

UPDATE sessions
SET routine_session_id = (
    SELECT rs.id
    FROM routine_sessions rs
    ORDER BY rs.position
    LIMIT 1 OFFSET (
        SELECT COUNT(*) - 1
        FROM sessions s2
        WHERE s2.date = sessions.date AND s2.id <= sessions.id
    )
)
WHERE routine_session_id IS NULL;
