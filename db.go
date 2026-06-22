package main

import (
	_ "embed"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/001_initial.sql
var migration001SQL string

//go:embed migrations/002_session_comments.sql
var migration002SQL string

//go:embed migrations/003_session_sleep_metrics.sql
var migration003SQL string

//go:embed migrations/004_outdoor_log.sql
var migration004SQL string

//go:embed migrations/005_outdoor_log_session_id.sql
var migration005SQL string

//go:embed migrations/006_session_toilet.sql
var migration006SQL string

//go:embed migrations/007_sleep_interrupted.sql
var migration007SQL string

//go:embed migrations/008_routine_session_id.sql
var migration008SQL string

type Phase string

const (
	PhaseActive   Phase = "ACTIVE"
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
	ID               int
	RoutineSessionID *int
	WokeAt           *time.Time
	SleptAt          *time.Time
	Comment          string
	SleepEase        string // "", "easy", "ok", "hard"
	Overtired        bool
	SleepInterrupted bool
	Toilet           string // "", "pee", "poop", "both", "nothing", "accident"
}

type DayStat struct {
	Date             string
	Cycles           int
	AvgAwakeMins     int
	AvgNapMins       int
	FirstWake        *time.Time
	LastSleep        *time.Time
	EasyCount        int
	OkCount          int
	HardCount        int
	OvertiredCount   int
	InterruptedCount int
	AccidentCount    int
}

type Config struct {
	PuppyName    string
	AwakeMinutes int
	NapMinutes   int
}

type RoutineSession struct {
	ID         int
	Position   int
	Label      string
	Activities []string
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
	for _, sql := range []string{migration001SQL, migration002SQL, migration003SQL, migration004SQL, migration005SQL, migration006SQL, migration007SQL, migration008SQL} {
		if err := runMigration(db, sql); err != nil {
			return err
		}
	}
	return nil
}

func runMigration(db *sql.DB, sql string) error {
	for _, stmt := range strings.Split(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue // ALTER TABLE ADD COLUMN on a column that already exists
			}
			snippet := stmt
			if len(snippet) > 60 {
				snippet = snippet[:60]
			}
			return fmt.Errorf("migration %q: %w", snippet, err)
		}
	}
	return nil
}

// ── puppy_state ───────────────────────────────────────────────────────────────

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
		`UPDATE puppy_state SET phase = ?, phase_started_at = ? WHERE id = 1`,
		phase, nowUTC(),
	)
	return err
}

// ── sessions ──────────────────────────────────────────────────────────────────

func logWake(db *sql.DB, date string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE date = ?`, date).Scan(&count); err != nil {
		return err
	}
	var routineSessionID sql.NullInt64
	if err := db.QueryRow(`SELECT id FROM routine_sessions ORDER BY position LIMIT 1 OFFSET ?`, count).Scan(&routineSessionID); err != nil && err != sql.ErrNoRows {
		return err
	}
	_, err := db.Exec(
		`INSERT INTO sessions (date, woke_at, routine_session_id) VALUES (?, ?, ?)`,
		date, nowUTC(), routineSessionID,
	)
	return err
}

func logSleep(db *sql.DB) error {
	_, err := db.Exec(`
		UPDATE sessions SET slept_at = ?
		WHERE id = (SELECT id FROM sessions WHERE slept_at IS NULL ORDER BY id DESC LIMIT 1)
	`, nowUTC())
	return err
}

func adjustWakeTime(db *sql.DB, date string, deltaMinutes int) error {
	var wokeAtStr string
	err := db.QueryRow(
		`SELECT woke_at FROM sessions WHERE date = ? ORDER BY id DESC LIMIT 1`, date,
	).Scan(&wokeAtStr)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := parseTimestamp(wokeAtStr)
	if err != nil {
		return err
	}
	adjusted := t.Add(time.Duration(deltaMinutes) * time.Minute)
	ts := adjusted.UTC().Format("2006-01-02 15:04:05")
	if _, err = db.Exec(`
		UPDATE sessions SET woke_at = ?
		WHERE id = (SELECT id FROM sessions WHERE date = ? ORDER BY id DESC LIMIT 1)
	`, ts, date); err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE puppy_state SET phase_started_at = ?`, ts)
	return err
}

func adjustSleepTime(db *sql.DB, deltaMinutes int) error {
	var sleptAtStr sql.NullString
	err := db.QueryRow(
		`SELECT slept_at FROM sessions WHERE slept_at IS NOT NULL ORDER BY id DESC LIMIT 1`,
	).Scan(&sleptAtStr)
	if err == sql.ErrNoRows || !sleptAtStr.Valid {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := parseTimestamp(sleptAtStr.String)
	if err != nil {
		return err
	}
	adjusted := t.Add(time.Duration(deltaMinutes) * time.Minute)
	ts := adjusted.UTC().Format("2006-01-02 15:04:05")
	if _, err = db.Exec(`
		UPDATE sessions SET slept_at = ?
		WHERE id = (SELECT id FROM sessions WHERE slept_at IS NOT NULL ORDER BY id DESC LIMIT 1)
	`, ts); err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE puppy_state SET phase_started_at = ?`, ts)
	return err
}

// closeStaleSession closes any open session from a previous calendar day by
// capping slept_at at 23:59:59 of that day, then sets phase to SLEEPING.
// This handles the case where the user never pressed "Put to Sleep" before midnight.
func closeStaleSession(db *sql.DB) error {
	var id int
	var dateStr string
	err := db.QueryRow(
		`SELECT id, date FROM sessions WHERE slept_at IS NULL ORDER BY id DESC LIMIT 1`,
	).Scan(&id, &dateStr)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")
	if dateStr >= today {
		return nil
	}
	closeAt := dateStr + " 23:59:59"
	if _, err := db.Exec(`UPDATE sessions SET slept_at = ? WHERE id = ?`, closeAt, id); err != nil {
		return err
	}
	return setPhase(db, PhaseSleeping)
}

func getSessionsForDate(db *sql.DB, date string) ([]DBSession, error) {
	rows, err := db.Query(`
		SELECT id, routine_session_id, woke_at, slept_at,
		       COALESCE(comment, ''),
		       COALESCE(sleep_ease, ''),
		       COALESCE(overtired, 0),
		       COALESCE(sleep_interrupted, 0),
		       COALESCE(toilet, '')
		FROM sessions WHERE date = ? ORDER BY id ASC`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []DBSession
	for rows.Next() {
		var s DBSession
		var wokeAt string
		var sleptAt sql.NullString
		var routineSessionID sql.NullInt64
		var overtiredInt, sleepInterruptedInt int
		if err := rows.Scan(&s.ID, &routineSessionID, &wokeAt, &sleptAt, &s.Comment, &s.SleepEase, &overtiredInt, &sleepInterruptedInt, &s.Toilet); err != nil {
			return nil, err
		}
		if routineSessionID.Valid {
			id := int(routineSessionID.Int64)
			s.RoutineSessionID = &id
		}
		s.Overtired = overtiredInt == 1
		s.SleepInterrupted = sleepInterruptedInt == 1
		if t, err := parseTimestamp(wokeAt); err == nil {
			s.WokeAt = &t
		}
		if sleptAt.Valid {
			if t, err := parseTimestamp(sleptAt.String); err == nil {
				s.SleptAt = &t
			}
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func setSleepEase(db *sql.DB, id int, ease string) error {
	_, err := db.Exec(`UPDATE sessions SET sleep_ease = ? WHERE id = ?`, ease, id)
	return err
}

func toggleSessionBool(db *sql.DB, id int, column string) error {
	_, err := db.Exec(
		fmt.Sprintf(`UPDATE sessions SET %s = CASE WHEN %s = 1 THEN 0 ELSE 1 END WHERE id = ?`, column, column),
		id,
	)
	return err
}

func setSessionComment(db *sql.DB, id int, comment string) error {
	_, err := db.Exec(`UPDATE sessions SET comment = ? WHERE id = ?`, comment, id)
	return err
}

// setSessionTime updates woke_at or slept_at, preserving the existing date and
// replacing only the hour/minute with newTime (interpreted in local time).
func setSessionTime(db *sql.DB, id int, column string, newTime time.Time) error {
	var raw string
	if err := db.QueryRow(
		fmt.Sprintf(`SELECT COALESCE(%s, '') FROM sessions WHERE id = ?`, column), id,
	).Scan(&raw); err != nil {
		return err
	}
	existing, err := parseTimestamp(raw)
	if err != nil {
		return fmt.Errorf("parse existing %s: %w", column, err)
	}
	local := existing.Local()
	combined := time.Date(local.Year(), local.Month(), local.Day(),
		newTime.Hour(), newTime.Minute(), 0, 0, time.Local)
	_, err = db.Exec(
		fmt.Sprintf(`UPDATE sessions SET %s = ? WHERE id = ?`, column),
		combined.UTC().Format("2006-01-02 15:04:05"), id,
	)
	return err
}

func getDayStats(db *sql.DB) ([]DayStat, error) {
	rows, err := db.Query(`
		SELECT
			date,
			COUNT(*) AS cycles,
			CAST(AVG(strftime('%s', slept_at) - strftime('%s', woke_at)) AS INTEGER) AS avg_awake_secs,
			MIN(woke_at) AS first_wake,
			MAX(slept_at) AS last_sleep,
			SUM(CASE WHEN sleep_ease = 'easy'    THEN 1 ELSE 0 END) AS easy_count,
			SUM(CASE WHEN sleep_ease = 'ok'      THEN 1 ELSE 0 END) AS ok_count,
			SUM(CASE WHEN sleep_ease = 'hard'    THEN 1 ELSE 0 END) AS hard_count,
			SUM(CASE WHEN overtired = 1          THEN 1 ELSE 0 END) AS overtired_count,
			SUM(CASE WHEN sleep_interrupted = 1  THEN 1 ELSE 0 END) AS interrupted_count,
			SUM(CASE WHEN toilet = 'accident'    THEN 1 ELSE 0 END) AS accident_count
		FROM sessions
		WHERE slept_at IS NOT NULL
		GROUP BY date
		ORDER BY date DESC
		LIMIT 30
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	today := time.Now().Format("2006-01-02")

	var days []DayStat
	for rows.Next() {
		var d DayStat
		var avgSecs int
		var firstWake, lastSleep string
		if err := rows.Scan(&d.Date, &d.Cycles, &avgSecs, &firstWake, &lastSleep,
			&d.EasyCount, &d.OkCount, &d.HardCount, &d.OvertiredCount,
			&d.InterruptedCount, &d.AccidentCount); err != nil {
			return nil, err
		}
		d.AvgAwakeMins = avgSecs / 60
		if t, err := parseTimestamp(firstWake); err == nil {
			tl := t.Local()
			d.FirstWake = &tl
		}
		if t, err := parseTimestamp(lastSleep); err == nil {
			tl := t.Local()
			d.LastSleep = &tl
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Compute avg nap duration (gap between consecutive sessions on the same day).
	napRows, err := db.Query(`
		SELECT date, CAST(AVG(nap_secs) AS INTEGER) FROM (
			SELECT s1.date,
			       CAST(strftime('%s', s2.woke_at) AS INTEGER) - CAST(strftime('%s', s1.slept_at) AS INTEGER) AS nap_secs
			FROM sessions s1
			INNER JOIN sessions s2
			       ON  s2.date = s1.date
			       AND s2.id   = (SELECT MIN(id) FROM sessions WHERE date = s1.date AND id > s1.id AND woke_at IS NOT NULL)
			WHERE s1.slept_at IS NOT NULL AND nap_secs > 0
		) GROUP BY date
	`)
	if err != nil {
		return nil, err
	}
	defer napRows.Close()
	napMins := make(map[string]int)
	for napRows.Next() {
		var date string
		var secs int
		if err := napRows.Scan(&date, &secs); err != nil {
			return nil, err
		}
		napMins[date] = secs / 60
	}
	if err := napRows.Err(); err != nil {
		return nil, err
	}
	for i := range days {
		days[i].AvgNapMins = napMins[days[i].Date]
	}

	// For today, only show LastSleep ("Went to bed") once all routine sessions are
	// completed — otherwise it would show a mid-day nap time as bedtime.
	var routineCount, completedToday int
	_ = db.QueryRow(`SELECT COUNT(*) FROM routine_sessions`).Scan(&routineCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE date = ? AND slept_at IS NOT NULL`, today).Scan(&completedToday)
	if completedToday < routineCount {
		for i := range days {
			if days[i].Date == today {
				days[i].LastSleep = nil
				break
			}
		}
	}

	return days, nil
}

func setSessionToilet(db *sql.DB, id int, value string) error {
	_, err := db.Exec(`UPDATE sessions SET toilet = ? WHERE id = ?`, value, id)
	return err
}

// ── routine_sessions ──────────────────────────────────────────────────────────

func getRoutineSessions(db *sql.DB) ([]RoutineSession, error) {
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

func createRoutineSession(db *sql.DB) error {
	var maxPos int
	db.QueryRow(`SELECT COALESCE(MAX(position), 0) FROM routine_sessions`).Scan(&maxPos)
	_, err := db.Exec(
		`INSERT INTO routine_sessions (position, label, activities) VALUES (?, ?, ?)`,
		maxPos+1, "New session", "",
	)
	return err
}

func updateRoutineSession(db *sql.DB, id int, label, activitiesText string) error {
	_, err := db.Exec(
		`UPDATE routine_sessions SET label = ?, activities = ? WHERE id = ?`,
		strings.TrimSpace(label), activitiesText, id,
	)
	return err
}

func deleteRoutineSession(db *sql.DB, id int) error {
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

func moveRoutineSession(db *sql.DB, id, dir int) error {
	var pos int
	if err := db.QueryRow(`SELECT position FROM routine_sessions WHERE id = ?`, id).Scan(&pos); err != nil {
		return err
	}
	neighborPos := pos + dir
	var neighborID int
	err := db.QueryRow(`SELECT id FROM routine_sessions WHERE position = ?`, neighborPos).Scan(&neighborID)
	if err == sql.ErrNoRows {
		return nil // already at edge
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

// ── config ────────────────────────────────────────────────────────────────────

func getConfig(db *sql.DB) (*Config, error) {
	rows, err := db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	c := &Config{PuppyName: "Nova", AwakeMinutes: 40, NapMinutes: 90}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		switch k {
		case "puppy_name":
			if v != "" {
				c.PuppyName = v
			}
		case "awake_minutes":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.AwakeMinutes = n
			}
		case "nap_minutes":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.NapMinutes = n
			}
		}
	}
	return c, rows.Err()
}

func saveConfig(db *sql.DB, c *Config) error {
	pairs := [][2]string{
		{"puppy_name", c.PuppyName},
		{"awake_minutes", strconv.Itoa(c.AwakeMinutes)},
		{"nap_minutes", strconv.Itoa(c.NapMinutes)},
	}
	for _, kv := range pairs {
		_, err := db.Exec(
			`INSERT INTO config (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			kv[0], kv[1],
		)
		if err != nil {
			return err
		}
	}
	return nil
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
		VALUES (?, ?, ?, ?)
		ON CONFLICT(date, meal_type) DO UPDATE SET
			amount     = excluded.amount,
			updated_at = excluded.updated_at
	`, date, mealType, amount, nowUTC())
	return err
}

// ── helpers ───────────────────────────────────────────────────────────────────

func nowUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func parseTimestamp(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func splitActivities(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func joinActivities(acts []string) string {
	return strings.Join(acts, "\n")
}
