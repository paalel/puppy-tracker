package simple

import (
	"database/sql"
	"time"

	"puppy/store"
)

// Home-alone in the simplified app reuses the classic sessions alone rows:
// woke_at = start, slept_at = end, alone = 1, with the alone_* columns and the
// comment column for the note. Values are the same canonical lowercase tokens
// classic uses (cage/pen/roaming, well/some/none, calm/unsettled/stressed) so
// the data stays consistent across both versions.

type AloneSession struct {
	ID        int
	Started   time.Time // local
	Ended     *time.Time
	Where     string
	Sleep     string
	Behaviour string
	Destroyed bool
	Note      string
}

func (a AloneSession) Running() bool { return a.Ended == nil }

func (a AloneSession) Minutes() int {
	end := a.Ended
	if end == nil {
		now := time.Now()
		end = &now
	}
	return int(end.Sub(a.Started).Minutes())
}

// IsCalm reports whether the session counts toward the "longest calm" record.
func (a AloneSession) IsCalm() bool { return a.Behaviour == "calm" && !a.Destroyed }

const aloneCols = `id, woke_at, slept_at, COALESCE(alone_location,''), COALESCE(alone_slept,''),
	COALESCE(alone_behaviour,''), COALESCE(alone_destroyed,0), COALESCE(comment,'')`

func scanAlone(s rowScanner) (*AloneSession, error) {
	var a AloneSession
	var started string
	var ended sql.NullString
	var destroyed int
	if err := s.Scan(&a.ID, &started, &ended, &a.Where, &a.Sleep, &a.Behaviour, &destroyed, &a.Note); err != nil {
		return nil, err
	}
	if t, err := store.ParseTimestamp(started); err == nil {
		a.Started = t.Local()
	}
	if ended.Valid {
		if t, err := store.ParseTimestamp(ended.String); err == nil {
			lt := t.Local()
			a.Ended = &lt
		}
	}
	a.Destroyed = destroyed == 1
	return &a, nil
}

func runningAlone(db *sql.DB) (*AloneSession, error) {
	a, err := scanAlone(db.QueryRow(
		`SELECT ` + aloneCols + ` FROM sessions
		 WHERE alone = 1 AND slept_at IS NULL AND deleted_at IS NULL ORDER BY id DESC LIMIT 1`,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func aloneByID(db *sql.DB, id int) (*AloneSession, error) {
	a, err := scanAlone(db.QueryRow(`SELECT `+aloneCols+` FROM sessions WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func startAlone(db *sql.DB) (int, error) {
	if a, err := runningAlone(db); err != nil {
		return 0, err
	} else if a != nil {
		return a.ID, nil
	}
	res, err := db.Exec(
		`INSERT INTO sessions (date, woke_at, alone) VALUES (?, ?, 1)`,
		store.RolloverDate(), store.NowUTC(),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func stopAlone(db *sql.DB) (int, int, error) {
	a, err := runningAlone(db)
	if err != nil || a == nil {
		return 0, 0, err
	}
	if _, err := db.Exec(`UPDATE sessions SET slept_at = ? WHERE id = ?`, store.NowUTC(), a.ID); err != nil {
		return 0, 0, err
	}
	a2, err := aloneByID(db, a.ID)
	if err != nil || a2 == nil {
		return a.ID, 0, err
	}
	return a.ID, a2.Minutes(), nil
}

func resumeAlone(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sessions SET slept_at = NULL WHERE id = ?`, id)
	return err
}

// setAloneChoice sets one of the single-choice fields with toggle semantics:
// setting the already-selected value clears it. column is an internal constant.
func setAloneChoice(db *sql.DB, id int, column, value string) error {
	var current string
	if err := db.QueryRow(`SELECT COALESCE(`+column+`,'') FROM sessions WHERE id = ?`, id).Scan(&current); err != nil {
		return err
	}
	if current == value {
		value = ""
	}
	_, err := db.Exec(`UPDATE sessions SET `+column+` = ? WHERE id = ?`, value, id)
	return err
}

func toggleAloneDestroyed(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sessions SET alone_destroyed = CASE WHEN alone_destroyed = 1 THEN 0 ELSE 1 END WHERE id = ?`, id)
	return err
}

func setAloneNote(db *sql.DB, id int, note string) error {
	_, err := db.Exec(`UPDATE sessions SET comment = ? WHERE id = ?`, note, id)
	return err
}

func deleteAlone(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sessions SET deleted_at = ? WHERE id = ? AND alone = 1`, store.NowUTC(), id)
	return err
}

func restoreAlone(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sessions SET deleted_at = NULL WHERE id = ?`, id)
	return err
}

// aloneForDay returns the day's alone sessions (newest first), running one included.
func aloneForDay(db *sql.DB, day string) ([]AloneSession, error) {
	rows, err := db.Query(
		`SELECT `+aloneCols+` FROM sessions
		 WHERE alone = 1 AND date = ? AND deleted_at IS NULL ORDER BY woke_at DESC`, day,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AloneSession
	for rows.Next() {
		a, err := scanAlone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ── Records ───────────────────────────────────────────────────────────────

type AloneRecord struct {
	Mins int
	Date time.Time // local start date of the record-holding session
	ID   int
}

func (r AloneRecord) Set() bool { return r.Mins > 0 }

type AloneRecords struct {
	Calm    AloneRecord
	Overall AloneRecord
}

// records scans all completed, non-deleted alone sessions for the longest calm
// (calm + not destroyed) and longest overall. excludeID skips the running session.
func records(db *sql.DB, excludeID int) (AloneRecords, error) {
	rows, err := db.Query(
		`SELECT ` + aloneCols + ` FROM sessions
		 WHERE alone = 1 AND woke_at IS NOT NULL AND slept_at IS NOT NULL AND deleted_at IS NULL`,
	)
	if err != nil {
		return AloneRecords{}, err
	}
	defer rows.Close()
	var rec AloneRecords
	for rows.Next() {
		a, err := scanAlone(rows)
		if err != nil {
			return AloneRecords{}, err
		}
		if a.ID == excludeID {
			continue
		}
		m := a.Minutes()
		if m > rec.Overall.Mins {
			rec.Overall = AloneRecord{Mins: m, Date: a.Started, ID: a.ID}
		}
		if a.IsCalm() && m > rec.Calm.Mins {
			rec.Calm = AloneRecord{Mins: m, Date: a.Started, ID: a.ID}
		}
	}
	return rec, rows.Err()
}
