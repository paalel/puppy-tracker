package simple

import (
	"database/sql"
	"encoding/json"
	"time"

	"puppy/store"
)

// ── Poops ───────────────────────────────────────────────────────────────────

type Poop struct {
	ID   int
	At   time.Time // local
	Kind string    // "poop" | "accident"
}

func (p Poop) IsAccident() bool { return p.Kind == "accident" }

func addPoop(db *sql.DB, kind string) (int, error) {
	if kind != "accident" {
		kind = "poop"
	}
	res, err := db.Exec(
		`INSERT INTO poops (day, at, kind) VALUES (?, ?, ?)`,
		store.RolloverDate(), store.NowUTC(), kind,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func listPoops(db *sql.DB, day string) ([]Poop, error) {
	rows, err := db.Query(
		`SELECT id, at, kind FROM poops WHERE day = ? AND deleted_at IS NULL ORDER BY at ASC`, day,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Poop
	for rows.Next() {
		var p Poop
		var at string
		if err := rows.Scan(&p.ID, &at, &p.Kind); err != nil {
			return nil, err
		}
		if t, err := store.ParseTimestamp(at); err == nil {
			p.At = t.Local()
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func softDelete(db *sql.DB, table string, id int) error {
	// table is an internal constant, never user input.
	_, err := db.Exec(`UPDATE `+table+` SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, store.NowUTC(), id)
	return err
}

func restore(db *sql.DB, table string, id int) error {
	_, err := db.Exec(`UPDATE `+table+` SET deleted_at = NULL WHERE id = ?`, id)
	return err
}

func poopKind(db *sql.DB, id int) string {
	var kind string
	db.QueryRow(`SELECT kind FROM poops WHERE id = ?`, id).Scan(&kind)
	return kind
}

// ── Naps ────────────────────────────────────────────────────────────────────

type Nap struct {
	ID      int
	Started time.Time  // local
	Ended   *time.Time // nil while running
}

func (n Nap) Running() bool { return n.Ended == nil }

func (n Nap) Minutes() int {
	end := n.Ended
	if end == nil {
		now := time.Now()
		end = &now
	}
	return int(end.Sub(n.Started).Minutes())
}

// runningNap returns the single open nap (ended_at NULL), or nil.
func runningNap(db *sql.DB) (*Nap, error) {
	n, err := scanNap(db.QueryRow(
		`SELECT id, started_at, ended_at FROM naps WHERE ended_at IS NULL AND deleted_at IS NULL ORDER BY id DESC LIMIT 1`,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

// startNap opens a nap now. Returns the running nap's id; if one is already
// running it returns that id without inserting (idempotent).
func startNap(db *sql.DB) (int, error) {
	if n, err := runningNap(db); err != nil {
		return 0, err
	} else if n != nil {
		return n.ID, nil
	}
	res, err := db.Exec(`INSERT INTO naps (day, started_at) VALUES (?, ?)`, store.RolloverDate(), store.NowUTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// stopNap closes the running nap. Returns its id and duration in minutes.
func stopNap(db *sql.DB) (int, int, error) {
	n, err := runningNap(db)
	if err != nil || n == nil {
		return 0, 0, err
	}
	if _, err := db.Exec(`UPDATE naps SET ended_at = ? WHERE id = ?`, store.NowUTC(), n.ID); err != nil {
		return 0, 0, err
	}
	n2, err := napByID(db, n.ID)
	if err != nil || n2 == nil {
		return n.ID, 0, err
	}
	return n.ID, n2.Minutes(), nil
}

// resumeNap re-opens a stopped nap (undo of stop).
func resumeNap(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE naps SET ended_at = NULL WHERE id = ?`, id)
	return err
}

func listNaps(db *sql.DB, day string) ([]Nap, error) {
	rows, err := db.Query(
		`SELECT id, started_at, ended_at FROM naps WHERE day = ? AND deleted_at IS NULL ORDER BY started_at ASC`, day,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Nap
	for rows.Next() {
		n, err := scanNapRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func napByID(db *sql.DB, id int) (*Nap, error) {
	n, err := scanNap(db.QueryRow(`SELECT id, started_at, ended_at FROM naps WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

type rowScanner interface{ Scan(...any) error }

func scanNap(s rowScanner) (*Nap, error) {
	var n Nap
	var started string
	var ended sql.NullString
	if err := s.Scan(&n.ID, &started, &ended); err != nil {
		return nil, err
	}
	if t, err := store.ParseTimestamp(started); err == nil {
		n.Started = t.Local()
	}
	if ended.Valid {
		if t, err := store.ParseTimestamp(ended.String); err == nil {
			lt := t.Local()
			n.Ended = &lt
		}
	}
	return &n, nil
}

func scanNapRows(rows *sql.Rows) (*Nap, error) { return scanNap(rows) }

// ── Outings ─────────────────────────────────────────────────────────────────

type Outing struct {
	ID    int
	At    time.Time // local
	Where []string
	What  []string
}

func createOuting(db *sql.DB, where, what []string) (int, error) {
	res, err := db.Exec(
		`INSERT INTO outings (day, at, where_json, what_json) VALUES (?, ?, ?, ?)`,
		store.RolloverDate(), store.NowUTC(), toJSON(where), toJSON(what),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func updateOuting(db *sql.DB, id int, where, what []string) error {
	_, err := db.Exec(
		`UPDATE outings SET where_json = ?, what_json = ? WHERE id = ?`,
		toJSON(where), toJSON(what), id,
	)
	return err
}

func listOutings(db *sql.DB, day string) ([]Outing, error) {
	rows, err := db.Query(
		`SELECT id, at, where_json, what_json FROM outings WHERE day = ? AND deleted_at IS NULL ORDER BY at ASC`, day,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outing
	for rows.Next() {
		o, err := scanOuting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func outingByID(db *sql.DB, id int) (*Outing, error) {
	o, err := scanOuting(db.QueryRow(`SELECT id, at, where_json, what_json FROM outings WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func scanOuting(s rowScanner) (*Outing, error) {
	var o Outing
	var at, whereJSON, whatJSON string
	if err := s.Scan(&o.ID, &at, &whereJSON, &whatJSON); err != nil {
		return nil, err
	}
	if t, err := store.ParseTimestamp(at); err == nil {
		o.At = t.Local()
	}
	o.Where = fromJSON(whereJSON)
	o.What = fromJSON(whatJSON)
	return &o, nil
}

func toJSON(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func fromJSON(s string) []string {
	var v []string
	_ = json.Unmarshal([]byte(s), &v)
	return v
}
