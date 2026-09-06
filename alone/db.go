package alone

import (
	"database/sql"

	"puppy/store"
)

// openSession starts a new alone session unless one is already open. Returns the
// open session's id either way.
func openSession(db *sql.DB) (int, error) {
	if id, err := openSessionID(db); err != nil {
		return 0, err
	} else if id != 0 {
		return id, nil
	}
	res, err := db.Exec(`INSERT INTO pen_sessions (started_at) VALUES (?)`, store.NowUTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// closeOpenSession stamps ended_at on the currently-open session, if any.
func closeOpenSession(db *sql.DB) error {
	_, err := db.Exec(
		`UPDATE pen_sessions SET ended_at = ? WHERE ended_at IS NULL`,
		store.NowUTC(),
	)
	return err
}

// addMarker records a marker against the open session. No-op (no error) if none
// is open or the kind is unknown.
func addMarker(db *sql.DB, kind string) error {
	if !validKind(kind) {
		return nil
	}
	id, err := openSessionID(db)
	if err != nil || id == 0 {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO pen_events (pen_session_id, occurred_at, kind) VALUES (?, ?, ?)`,
		id, store.NowUTC(), kind,
	)
	return err
}

func openSessionID(db *sql.DB) (int, error) {
	var id sql.NullInt64
	err := db.QueryRow(`SELECT id FROM pen_sessions WHERE ended_at IS NULL ORDER BY id DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return int(id.Int64), nil
}

// getOpenSession returns the currently-open session with its markers, or nil.
func getOpenSession(db *sql.DB) (*Session, error) {
	id, err := openSessionID(db)
	if err != nil || id == 0 {
		return nil, err
	}
	return getSession(db, id)
}

// getRecentSessions returns the most recently-ended sessions, newest first.
func getRecentSessions(db *sql.DB, limit int) ([]Session, error) {
	rows, err := db.Query(
		`SELECT id FROM pen_sessions WHERE ended_at IS NOT NULL ORDER BY started_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sessions := make([]Session, 0, len(ids))
	for _, id := range ids {
		s, err := getSession(db, id)
		if err != nil {
			return nil, err
		}
		if s != nil {
			sessions = append(sessions, *s)
		}
	}
	return sessions, nil
}

func getSession(db *sql.DB, id int) (*Session, error) {
	var startedRaw string
	var endedRaw sql.NullString
	err := db.QueryRow(
		`SELECT started_at, ended_at FROM pen_sessions WHERE id = ?`, id,
	).Scan(&startedRaw, &endedRaw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	started, err := store.ParseTimestamp(startedRaw)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: id, StartedAt: started}
	if endedRaw.Valid {
		if t, err := store.ParseTimestamp(endedRaw.String); err == nil {
			s.EndedAt = &t
		}
	}

	rows, err := db.Query(
		`SELECT occurred_at, kind FROM pen_events WHERE pen_session_id = ? ORDER BY occurred_at ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, kind string
		if err := rows.Scan(&raw, &kind); err != nil {
			return nil, err
		}
		if t, err := store.ParseTimestamp(raw); err == nil {
			s.Markers = append(s.Markers, Marker{Kind: kind, At: t})
		}
	}
	return s, rows.Err()
}
