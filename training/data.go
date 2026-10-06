// Package training is the simplified app's skill-training tracker: skills are
// configured in settings, and reps (each with a quality mark) are logged against
// a skill from the Today card. It's self-contained — data, the Today card, the
// rep endpoints, and the settings-config fragment all live here.
package training

import (
	"database/sql"
	"fmt"
	"time"

	"puppy/store"
)

// Quality marks, best-known order.
var Qualities = []string{"bad", "okay", "good", "excellent"}

func validQuality(q string) bool {
	for _, v := range Qualities {
		if v == q {
			return true
		}
	}
	return false
}

// Period a skill's target is measured over.
const (
	PeriodDay  = "day"
	PeriodWeek = "week"
)

type Skill struct {
	ID          int
	Name        string
	Instruction string
	Target      int
	Period      string
}

func (s Skill) Weekly() bool { return s.Period == PeriodWeek }

// ── Skills (config) ───────────────────────────────────────────────────────

func listSkills(db *sql.DB) ([]Skill, error) {
	rows, err := db.Query(
		`SELECT id, name, instruction, target, period FROM training_skills
		 WHERE deleted_at IS NULL ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		var s Skill
		if err := rows.Scan(&s.ID, &s.Name, &s.Instruction, &s.Target, &s.Period); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func createSkill(db *sql.DB, name, instruction string, target int, period string) error {
	if target < 1 {
		target = 1
	}
	if period != PeriodWeek {
		period = PeriodDay
	}
	var pos int
	db.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM training_skills WHERE deleted_at IS NULL`).Scan(&pos)
	_, err := db.Exec(
		`INSERT INTO training_skills (name, instruction, target, period, position) VALUES (?, ?, ?, ?, ?)`,
		name, instruction, target, period, pos,
	)
	return err
}

func updateSkill(db *sql.DB, id int, name, instruction string, target int, period string) error {
	if target < 1 {
		target = 1
	}
	if period != PeriodWeek {
		period = PeriodDay
	}
	_, err := db.Exec(
		`UPDATE training_skills SET name = ?, instruction = ?, target = ?, period = ? WHERE id = ?`,
		name, instruction, target, period, id,
	)
	return err
}

func deleteSkill(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE training_skills SET deleted_at = ? WHERE id = ?`, store.NowUTC(), id)
	return err
}

// ── Reps ──────────────────────────────────────────────────────────────────

func addRep(db *sql.DB, skillID int, quality string) (int, error) {
	if !validQuality(quality) {
		return 0, fmt.Errorf("invalid quality %q", quality)
	}
	res, err := db.Exec(
		`INSERT INTO training_reps (day, at, skill_id, quality) VALUES (?, ?, ?, ?)`,
		store.RolloverDate(), store.NowUTC(), skillID, quality,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func deleteRep(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE training_reps SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, store.NowUTC(), id)
	return err
}

func restoreRep(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE training_reps SET deleted_at = NULL WHERE id = ?`, id)
	return err
}

type Rep struct {
	ID      int
	At      time.Time // local
	SkillID int
	Quality string
}

// repsForSkillDay returns a skill's reps logged on a given day, oldest first.
func repsForSkillDay(db *sql.DB, skillID int, day string) ([]Rep, error) {
	rows, err := db.Query(
		`SELECT id, at, quality FROM training_reps
		 WHERE skill_id = ? AND day = ? AND deleted_at IS NULL ORDER BY at ASC`, skillID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rep
	for rows.Next() {
		r := Rep{SkillID: skillID}
		var at string
		if err := rows.Scan(&r.ID, &at, &r.Quality); err != nil {
			return nil, err
		}
		if t, err := store.ParseTimestamp(at); err == nil {
			r.At = t.Local()
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// progressCount counts a skill's reps over its target period relative to `day`:
// the day itself for day-skills, or the Monday-based week containing it for
// week-skills.
func progressCount(db *sql.DB, skill Skill, day string) (int, error) {
	var n int
	if skill.Weekly() {
		err := db.QueryRow(
			`SELECT COUNT(*) FROM training_reps WHERE skill_id = ? AND deleted_at IS NULL AND day >= ? AND day <= ?`,
			skill.ID, weekStart(day), day,
		).Scan(&n)
		return n, err
	}
	err := db.QueryRow(
		`SELECT COUNT(*) FROM training_reps WHERE skill_id = ? AND deleted_at IS NULL AND day = ?`,
		skill.ID, day,
	).Scan(&n)
	return n, err
}

// weekStart returns the Monday (as a date string) of the week containing day.
func weekStart(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	// Go: Sunday=0..Saturday=6; shift so Monday is the week's first day.
	offset := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -offset).Format("2006-01-02")
}

func skillName(db *sql.DB, id int) string {
	var name string
	db.QueryRow(`SELECT name FROM training_skills WHERE id = ?`, id).Scan(&name)
	return name
}
