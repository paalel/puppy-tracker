package main

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations
var migrationsFS embed.FS

type Phase string

const (
	PhaseActive   Phase = "ACTIVE"
	PhaseCrating  Phase = "CRATING"
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
	CrateAt          *time.Time
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
	AvgSettleMins    int
	NightMins        int
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

type ChartPoint struct {
	X string `json:"x"`
	Y int    `json:"y"`
}

type SessionSeries struct {
	Awake  []ChartPoint
	Nap    []ChartPoint
	Settle []ChartPoint
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
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		sql, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return err
		}
		if err := runMigration(db, string(sql)); err != nil {
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

// ── state (derived from sessions) ─────────────────────────────────────────────

func getState(db *sql.DB) (*PuppyState, error) {
	var wokeAt, crateAt, sleptAt sql.NullString
	err := db.QueryRow(
		`SELECT woke_at, crate_at, slept_at FROM sessions ORDER BY date DESC, id DESC LIMIT 1`,
	).Scan(&wokeAt, &crateAt, &sleptAt)
	if err == sql.ErrNoRows {
		return &PuppyState{Phase: PhaseSleeping, PhaseStartedAt: time.Now()}, nil
	}
	if err != nil {
		return nil, err
	}
	if sleptAt.Valid {
		t, err := parseTimestamp(sleptAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse slept_at: %w", err)
		}
		return &PuppyState{Phase: PhaseSleeping, PhaseStartedAt: t}, nil
	}
	if crateAt.Valid {
		t, err := parseTimestamp(crateAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse crate_at: %w", err)
		}
		return &PuppyState{Phase: PhaseCrating, PhaseStartedAt: t}, nil
	}
	t, err := parseTimestamp(wokeAt.String)
	if err != nil {
		return nil, fmt.Errorf("parse woke_at: %w", err)
	}
	return &PuppyState{Phase: PhaseActive, PhaseStartedAt: t}, nil
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

func logCrate(db *sql.DB) error {
	_, err := db.Exec(`
		UPDATE sessions SET crate_at = ?
		WHERE id = (SELECT id FROM sessions WHERE crate_at IS NULL AND slept_at IS NULL ORDER BY id DESC LIMIT 1)
	`, nowUTC())
	return err
}

func logSleep(db *sql.DB) error {
	_, err := db.Exec(`
		UPDATE sessions SET slept_at = ?
		WHERE id = (SELECT id FROM sessions WHERE slept_at IS NULL ORDER BY id DESC LIMIT 1)
	`, nowUTC())
	return err
}

// adjustLatestSessionTime shifts the most recent non-null value of column by
// deltaMinutes. Safe: column is always a hardcoded internal string, not user input.
func adjustLatestSessionTime(db *sql.DB, column string, deltaMinutes int) error {
	var raw sql.NullString
	q := fmt.Sprintf(`SELECT %s FROM sessions WHERE %s IS NOT NULL ORDER BY id DESC LIMIT 1`, column, column)
	if err := db.QueryRow(q).Scan(&raw); err == sql.ErrNoRows || !raw.Valid {
		return nil
	} else if err != nil {
		return err
	}
	t, err := parseTimestamp(raw.String)
	if err != nil {
		return err
	}
	ts := t.Add(time.Duration(deltaMinutes) * time.Minute).UTC().Format("2006-01-02 15:04:05")
	u := fmt.Sprintf(`UPDATE sessions SET %s = ? WHERE id = (SELECT id FROM sessions WHERE %s IS NOT NULL ORDER BY id DESC LIMIT 1)`, column, column)
	_, err = db.Exec(u, ts)
	return err
}


// closeStaleSession closes any open session from a previous calendar day by
// capping crate_at and slept_at at 23:59:59 of that day.
// This handles the case where the user never pressed "Fell Asleep" before midnight.
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
	_, err = db.Exec(`
		UPDATE sessions SET
			crate_at = COALESCE(crate_at, ?),
			slept_at = ?
		WHERE id = ?
	`, closeAt, closeAt, id)
	return err
}

func getSessionsForDate(db *sql.DB, date string) ([]DBSession, error) {
	rows, err := db.Query(`
		SELECT id, routine_session_id, woke_at, crate_at, slept_at,
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
		var crateAt, sleptAt sql.NullString
		var routineSessionID sql.NullInt64
		var overtiredInt, sleepInterruptedInt int
		if err := rows.Scan(&s.ID, &routineSessionID, &wokeAt, &crateAt, &sleptAt, &s.Comment, &s.SleepEase, &overtiredInt, &sleepInterruptedInt, &s.Toilet); err != nil {
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
		if crateAt.Valid {
			if t, err := parseTimestamp(crateAt.String); err == nil {
				s.CrateAt = &t
			}
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

// setSessionString sets a text column on a session row.
// Safe: column is always a hardcoded internal string, not user input.
func setSessionString(db *sql.DB, id int, column, value string) error {
	_, err := db.Exec(fmt.Sprintf(`UPDATE sessions SET %s = ? WHERE id = ?`, column), value, id)
	return err
}

func toggleSessionBool(db *sql.DB, id int, column string) error {
	_, err := db.Exec(
		fmt.Sprintf(`UPDATE sessions SET %s = CASE WHEN %s = 1 THEN 0 ELSE 1 END WHERE id = ?`, column, column),
		id,
	)
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

	napMins, err := queryDateMins(db, `
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
	for i := range days {
		days[i].AvgNapMins = napMins[days[i].Date]
	}

	settleMins, err := queryDateMins(db, `
		SELECT date, CAST(AVG(settle_secs) AS INTEGER) FROM (
			SELECT date,
			       CAST(strftime('%s', slept_at) AS INTEGER) - CAST(strftime('%s', crate_at) AS INTEGER) AS settle_secs
			FROM sessions
			WHERE crate_at IS NOT NULL AND slept_at IS NOT NULL AND settle_secs > 0
		) GROUP BY date
	`)
	if err != nil {
		return nil, err
	}
	for i := range days {
		days[i].AvgSettleMins = settleMins[days[i].Date]
	}

	nightMins, err := queryDateMins(db, `
		SELECT s1.date,
		       CAST(strftime('%s', s2.woke_at) AS INTEGER) - CAST(strftime('%s', s1.slept_at) AS INTEGER)
		FROM sessions s1
		JOIN sessions s2 ON s2.date = date(s1.date, '+1 day')
		WHERE s1.id = (SELECT MAX(id) FROM sessions s3 WHERE s3.date = s1.date AND s3.slept_at IS NOT NULL)
		  AND s2.id = (SELECT MIN(id) FROM sessions s4 WHERE s4.date = s2.date)
		  AND s1.slept_at IS NOT NULL AND s2.woke_at IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	for i := range days {
		days[i].NightMins = nightMins[days[i].Date]
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

// getSessionSeries returns per-session chart points for the last 30 days.
// Awake: woke_at → duration. Settle: crate_at → duration. Nap: slept_at → duration
// until next woke_at, but only within the same day (overnight sleeps excluded).
func getSessionSeries(db *sql.DB) (*SessionSeries, error) {
	rows, err := db.Query(`
		SELECT
			woke_at,
			crate_at,
			slept_at,
			CAST((strftime('%s', slept_at) - strftime('%s', woke_at)) / 60 AS INTEGER),
			CASE WHEN crate_at IS NOT NULL
			     THEN CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER)
			     ELSE NULL END,
			CASE WHEN LEAD(date) OVER (ORDER BY id) = date
			     THEN CAST((strftime('%s', LEAD(woke_at) OVER (ORDER BY id)) - strftime('%s', slept_at)) / 60 AS INTEGER)
			     ELSE NULL END
		FROM sessions
		WHERE woke_at IS NOT NULL AND slept_at IS NOT NULL
		  AND date >= date('now', '-30 days')
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s := &SessionSeries{}
	for rows.Next() {
		var wokeAt, sleptAt string
		var crateAt sql.NullString
		var awakeMins int
		var settleMins, napMins sql.NullInt64
		if err := rows.Scan(&wokeAt, &crateAt, &sleptAt, &awakeMins, &settleMins, &napMins); err != nil {
			return nil, err
		}
		s.Awake = append(s.Awake, ChartPoint{X: strings.Replace(wokeAt, " ", "T", 1), Y: awakeMins})
		if settleMins.Valid {
			s.Settle = append(s.Settle, ChartPoint{X: strings.Replace(crateAt.String, " ", "T", 1), Y: int(settleMins.Int64)})
		}
		if napMins.Valid {
			s.Nap = append(s.Nap, ChartPoint{X: strings.Replace(sleptAt, " ", "T", 1), Y: int(napMins.Int64)})
		}
	}
	return s, rows.Err()
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

// queryDateMins runs a query that returns (date, seconds) rows and converts
// the result into a map of date → minutes.
func queryDateMins(db *sql.DB, query string) (map[string]int, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int)
	for rows.Next() {
		var date string
		var secs int
		if err := rows.Scan(&date, &secs); err != nil {
			return nil, err
		}
		m[date] = secs / 60
	}
	return m, rows.Err()
}

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
