package routine

import (
	"database/sql"
	"strings"
)

func GetAll(db *sql.DB) ([]RoutineSession, error) {
	rows, err := db.Query(
		`SELECT id, position, label, activities FROM routine_sessions ORDER BY position ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []RoutineSession
	for rows.Next() {
		var s RoutineSession
		var acts string
		if err := rows.Scan(&s.ID, &s.Position, &s.Label, &acts); err != nil {
			return nil, err
		}
		s.Activities = splitActivities(acts)
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func create(db *sql.DB) error {
	var maxPos int
	db.QueryRow(`SELECT COALESCE(MAX(position), 0) FROM routine_sessions`).Scan(&maxPos)
	_, err := db.Exec(
		`INSERT INTO routine_sessions (position, label, activities) VALUES (?, ?, ?)`,
		maxPos+1, "New session", "",
	)
	return err
}

func update(db *sql.DB, id int, label, activitiesText string) error {
	_, err := db.Exec(
		`UPDATE routine_sessions SET label = ?, activities = ? WHERE id = ?`,
		strings.TrimSpace(label), activitiesText, id,
	)
	return err
}

func remove(db *sql.DB, id int) error {
	var pos int
	if err := db.QueryRow(`SELECT position FROM routine_sessions WHERE id = ?`, id).Scan(&pos); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM routine_sessions WHERE id = ?`, id); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE routine_sessions SET position = position - 1 WHERE position > ?`, pos)
	return err
}

func move(db *sql.DB, id, dir int) error {
	var pos int
	if err := db.QueryRow(`SELECT position FROM routine_sessions WHERE id = ?`, id).Scan(&pos); err != nil {
		return err
	}
	neighborPos := pos + dir
	var neighborID int
	err := db.QueryRow(`SELECT id FROM routine_sessions WHERE position = ?`, neighborPos).Scan(&neighborID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE routine_sessions SET position = ? WHERE id = ?`, neighborPos, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE routine_sessions SET position = ? WHERE id = ?`, pos, neighborID); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
