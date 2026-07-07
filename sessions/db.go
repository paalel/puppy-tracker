package sessions

import (
	"database/sql"
	"fmt"
	"time"

	"puppy/store"
)

func parseTimestamp(s string) (time.Time, error) { return store.ParseTimestamp(s) }
func nowUTC() string                             { return store.NowUTC() }

func getState(db *sql.DB) (*puppyState, error) {
	var wokeAt, crateAt, sleptAt sql.NullString
	err := db.QueryRow(
		`SELECT woke_at, crate_at, slept_at FROM sessions ORDER BY date DESC, id DESC LIMIT 1`,
	).Scan(&wokeAt, &crateAt, &sleptAt)
	if err == sql.ErrNoRows {
		return &puppyState{Phase: PhaseSleeping, PhaseStartedAt: time.Now()}, nil
	}
	if err != nil {
		return nil, err
	}
	if sleptAt.Valid {
		t, err := parseTimestamp(sleptAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse slept_at: %w", err)
		}
		return &puppyState{Phase: PhaseSleeping, PhaseStartedAt: t}, nil
	}
	if crateAt.Valid {
		t, err := parseTimestamp(crateAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse crate_at: %w", err)
		}
		return &puppyState{Phase: PhaseCrating, PhaseStartedAt: t}, nil
	}
	t, err := parseTimestamp(wokeAt.String)
	if err != nil {
		return nil, fmt.Errorf("parse woke_at: %w", err)
	}
	return &puppyState{Phase: PhaseActive, PhaseStartedAt: t}, nil
}

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

func undoPhase(db *sql.DB) error {
	var id int
	var crateAt, sleptAt sql.NullString
	err := db.QueryRow(`SELECT id, crate_at, slept_at FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&id, &crateAt, &sleptAt)
	if err != nil {
		return err
	}
	if sleptAt.Valid {
		_, err = db.Exec(`UPDATE sessions SET slept_at = NULL WHERE id = ?`, id)
	} else if crateAt.Valid {
		_, err = db.Exec(`UPDATE sessions SET crate_at = NULL WHERE id = ?`, id)
	} else {
		_, err = db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	}
	return err
}

func toggleToilet(db *sql.DB, id int, value string) error {
	var query string
	switch value {
	case "pee":
		query = `UPDATE sessions SET toilet_pee = 1 - toilet_pee WHERE id = ?`
	case "poop":
		query = `UPDATE sessions SET toilet_poop = 1 - toilet_poop WHERE id = ?`
	case "accident":
		query = `UPDATE sessions SET toilet_accident = 1 - toilet_accident WHERE id = ?`
	case "nothing":
		query = `UPDATE sessions SET toilet_pee = 0, toilet_poop = 0, toilet_accident = 0 WHERE id = ?`
	default:
		return fmt.Errorf("invalid toilet value: %s", value)
	}
	_, err := db.Exec(query, id)
	return err
}

func getSessionDate(db *sql.DB, id int) (string, error) {
	var date string
	err := db.QueryRow(`SELECT date FROM sessions WHERE id = ?`, id).Scan(&date)
	return date, err
}

func getSessionsForDate(db *sql.DB, date string) ([]dbSession, error) {
	rows, err := db.Query(`
		SELECT id, routine_session_id, woke_at, crate_at, slept_at,
		       COALESCE(comment, ''),
		       COALESCE(sleep_ease, ''),
		       COALESCE(overtired, 0),
		       COALESCE(toilet_pee, 0),
		       COALESCE(toilet_poop, 0),
		       COALESCE(toilet_accident, 0),
		       COALESCE(training_quality, ''),
		       COALESCE(physical_activity, 0),
		       COALESCE(mental_activity, 0),
		       COALESCE(calm_winddown, 0),
		       COALESCE(environmental_activity, 0),
		       COALESCE(excluded, 0)
		FROM sessions WHERE date = ? ORDER BY id ASC`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []dbSession
	for rows.Next() {
		var s dbSession
		var wokeAt string
		var crateAt, sleptAt sql.NullString
		var routineSessionID sql.NullInt64
		var overtiredInt, peeInt, poopInt, accidentInt int
		var physicalInt, mentalInt, calmInt, environmentalInt, excludedInt int
		if err := rows.Scan(
			&s.ID, &routineSessionID, &wokeAt, &crateAt, &sleptAt,
			&s.Comment, &s.SleepEase, &overtiredInt,
			&peeInt, &poopInt, &accidentInt,
			&s.TrainingQuality,
			&physicalInt, &mentalInt, &calmInt, &environmentalInt, &excludedInt,
		); err != nil {
			return nil, err
		}
		if routineSessionID.Valid {
			id := int(routineSessionID.Int64)
			s.RoutineSessionID = &id
		}
		s.Overtired = overtiredInt == 1
		s.ToiletPee = peeInt == 1
		s.ToiletPoop = poopInt == 1
		s.ToiletAccident = accidentInt == 1
		s.PhysicalActivity = physicalInt == 1
		s.MentalActivity = mentalInt == 1
		s.CalmWinddown = calmInt == 1
		s.EnvironmentalActivity = environmentalInt == 1
		s.Excluded = excludedInt == 1
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
		result = append(result, s)
	}
	return result, rows.Err()
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

// setSessionTime updates a time column, preserving the existing date and
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

func getPoopStatus(db *sql.DB) (*PoopStatus, error) {
	rows, err := db.Query(`
		SELECT woke_at FROM sessions
		WHERE toilet_poop = 1 AND woke_at IS NOT NULL
		ORDER BY woke_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var times []time.Time
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if t, err := parseTimestamp(s); err == nil {
			times = append(times, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ps := &PoopStatus{SampleSize: len(times)}
	if len(times) == 0 {
		return ps, nil
	}
	last := times[len(times)-1].Local()
	ps.LastPoop = &last

	if len(times) >= 2 {
		var totalSecs int64
		for i := 1; i < len(times); i++ {
			totalSecs += int64(times[i].Sub(times[i-1]).Seconds())
		}
		ps.AvgIntervalMins = int(totalSecs/int64(len(times)-1)) / 60
	}
	return ps, nil
}

func logNightToilet(db *sql.DB, toilet string) error {
	pee, poop, accident := 0, 0, 0
	switch toilet {
	case "pee":
		pee = 1
	case "poop":
		poop = 1
	case "accident":
		accident = 1
	}
	_, err := db.Exec(
		`INSERT INTO night_toilets (occurred_at, toilet, toilet_pee, toilet_poop, toilet_accident) VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC().Format("2006-01-02 15:04:05"), toilet, pee, poop, accident,
	)
	return err
}

func GetSessionsNeedingNotification(db *sql.DB, afterMinutes int) ([]int, error) {
	rows, err := db.Query(`
		SELECT id FROM sessions
		WHERE woke_at IS NOT NULL
		  AND slept_at IS NULL
		  AND awake_notified = 0
		  AND woke_at <= datetime('now', '-' || ? || ' minutes')
	`, afterMinutes)
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
	return ids, rows.Err()
}

func MarkSessionNotified(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sessions SET awake_notified = 1 WHERE id = ?`, id)
	return err
}
