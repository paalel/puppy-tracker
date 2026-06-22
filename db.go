package main

import (
	"database/sql"
	"fmt"
	"time"
)

type Phase string

const (
	PhaseActive   Phase = "ACTIVE"
	PhaseWindDown Phase = "WIND_DOWN"
	PhaseSleeping Phase = "SLEEPING"
)

type MealAmount string

const (
	AmountNothing    MealAmount = "nothing"
	AmountTooLittle  MealAmount = "too_little"
	AmountPrettyGood MealAmount = "pretty_good"
	AmountFullMeal   MealAmount = "full_meal"
)

type MealType string

const (
	MealBreakfast MealType = "breakfast"
	MealLunch     MealType = "lunch"
	MealDinner    MealType = "dinner"
)

type PuppyState struct {
	Phase          Phase
	PhaseStartedAt time.Time
}

type MealEntry struct {
	Type     MealType
	Label    string
	Deadline string
	Amount   MealAmount
}

type DBSession struct {
	ID      int
	WokeAt  *time.Time
	SleptAt *time.Time
}

var mealCatalog = []struct {
	Type     MealType
	Label    string
	Deadline string
}{
	{MealBreakfast, "Breakfast", "10:00"},
	{MealLunch, "Lunch", "14:30"},
	{MealDinner, "Dinner", "19:00"},
}

func initDB(db *sql.DB) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS puppy_state (
			id               INTEGER PRIMARY KEY CHECK (id = 1),
			phase            TEXT     NOT NULL DEFAULT 'SLEEPING',
			phase_started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT OR IGNORE INTO puppy_state (id, phase, phase_started_at)
		 VALUES (1, 'SLEEPING', CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			date     TEXT     NOT NULL,
			woke_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			slept_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS meals (
			date       TEXT NOT NULL,
			meal_type  TEXT NOT NULL,
			amount     TEXT NOT NULL DEFAULT 'nothing',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (date, meal_type)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("initDB %q: %w", s[:min(len(s), 40)], err)
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── puppy_state ──────────────────────────────────────────────────────────────

func getState(db *sql.DB) (*PuppyState, error) {
	var s PuppyState
	var startedAt string
	err := db.QueryRow(`SELECT phase, phase_started_at FROM puppy_state WHERE id = 1`).
		Scan(&s.Phase, &startedAt)
	if err != nil {
		return nil, err
	}
	s.PhaseStartedAt, err = parseTimestamp(startedAt)
	if err != nil {
		return nil, fmt.Errorf("parse phase_started_at %q: %w", startedAt, err)
	}
	return &s, nil
}

func setPhase(db *sql.DB, phase Phase) error {
	_, err := db.Exec(
		`UPDATE puppy_state SET phase = ?, phase_started_at = CURRENT_TIMESTAMP WHERE id = 1`,
		phase,
	)
	return err
}

// ── sessions ──────────────────────────────────────────────────────────────────

func logWake(db *sql.DB, date string) error {
	_, err := db.Exec(`INSERT INTO sessions (date, woke_at) VALUES (?, CURRENT_TIMESTAMP)`, date)
	return err
}

func logSleep(db *sql.DB, date string) error {
	_, err := db.Exec(`
		UPDATE sessions SET slept_at = CURRENT_TIMESTAMP
		WHERE id = (
			SELECT id FROM sessions WHERE date = ? AND slept_at IS NULL ORDER BY id DESC LIMIT 1
		)
	`, date)
	return err
}

func getSessionsForDate(db *sql.DB, date string) ([]DBSession, error) {
	rows, err := db.Query(
		`SELECT id, woke_at, slept_at FROM sessions WHERE date = ? ORDER BY id ASC`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []DBSession
	for rows.Next() {
		var s DBSession
		var wokeAt string
		var sleptAt sql.NullString
		if err := rows.Scan(&s.ID, &wokeAt, &sleptAt); err != nil {
			return nil, err
		}
		t, err := parseTimestamp(wokeAt)
		if err == nil {
			s.WokeAt = &t
		}
		if sleptAt.Valid {
			t2, err := parseTimestamp(sleptAt.String)
			if err == nil {
				s.SleptAt = &t2
			}
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ── meals ─────────────────────────────────────────────────────────────────────

func getMeals(db *sql.DB, date string) ([]MealEntry, error) {
	rows, err := db.Query(`SELECT meal_type, amount FROM meals WHERE date = ?`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recorded := make(map[MealType]MealAmount)
	for rows.Next() {
		var mt MealType
		var amt MealAmount
		if err := rows.Scan(&mt, &amt); err != nil {
			return nil, err
		}
		recorded[mt] = amt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	meals := make([]MealEntry, len(mealCatalog))
	for i, c := range mealCatalog {
		amt := AmountNothing
		if a, ok := recorded[c.Type]; ok {
			amt = a
		}
		meals[i] = MealEntry{Type: c.Type, Label: c.Label, Deadline: c.Deadline, Amount: amt}
	}
	return meals, nil
}

func setMeal(db *sql.DB, date string, mealType MealType, amount MealAmount) error {
	_, err := db.Exec(`
		INSERT INTO meals (date, meal_type, amount, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(date, meal_type) DO UPDATE SET
			amount     = excluded.amount,
			updated_at = excluded.updated_at
	`, date, mealType, amount)
	return err
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseTimestamp(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
