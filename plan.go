package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type SessionTemplate struct {
	Label      string
	Activities []string
}

// dailyPlan is the seed data for routine_sessions. Used only once on first run.
var dailyPlan = []SessionTemplate{
	{
		Label:      "Morning",
		Activities: []string{"Bathroom break outside", "Breakfast in playpen"},
	},
	{
		Label:      "Mid-morning",
		Activities: []string{"Environmental training in Oslo — carry or sit on bench (10–15 min)", "Calm walk home"},
	},
	{
		Label:      "Lunch",
		Activities: []string{"Lunch in playpen", "Calm cuddle"},
	},
	{
		Label:      "Afternoon",
		Activities: []string{"Mental training: sit / stay", "Solo time in playpen"},
	},
	{
		Label:      "Dinner",
		Activities: []string{"Dinner in playpen", "Calm wind-down"},
	},
	{
		Label:      "Evening",
		Activities: []string{"Evening snack in playpen", "Calm wind-down"},
	},
	{
		Label:      "Night outing",
		Activities: []string{"Last bathroom break — very calm, no play", "Cuddle on lap or floor only"},
	},
}

// baseWakeTimes[0] anchors the first session when no actual wake time exists.
var baseWakeTimes = []string{"09:00"}

type SessionView struct {
	ID             int
	Index          int
	Label          string
	Activities     []string
	PlannedWake    time.Time
	PlannedSleep   time.Time
	ActualWake     *time.Time
	ActualSleep    *time.Time
	IsPast         bool
	IsActive       bool
	IsFuture       bool
	ActualDuration  string
	DurationClass   string
	Comment         string
	SleepEase       string
	Overtired        bool
	ToiletPee       bool
	ToiletPoop      bool
	ToiletAccident  bool
	TrainingQuality  string
	PhysicalActivity bool
	MentalActivity   bool
	CalmWinddown     bool
	ActualCrate      *time.Time
	SleepDuration   string
	SettleDuration  string
}

// buildSchedule constructs the day's session list with planned times adjusted
// by actual data. Each session's planned wake = previous session's actual_slept_at
// + napMins, cascading forward through the day.
func buildSchedule(date string, dbSessions []DBSession, routineSessions []RoutineSession, cfg *Config) []SessionView {
	loc := time.Local
	today, _ := time.ParseInLocation("2006-01-02", date, loc)
	awake := time.Duration(cfg.AwakeMinutes) * time.Minute
	nap := time.Duration(cfg.NapMinutes) * time.Minute

	dbByRoutineID := make(map[int]DBSession, len(dbSessions))
	for _, s := range dbSessions {
		if s.RoutineSessionID != nil {
			dbByRoutineID[*s.RoutineSessionID] = s
		}
	}

	views := make([]SessionView, len(routineSessions))

	for i, rs := range routineSessions {
		var dbSess *DBSession
		if s, ok := dbByRoutineID[rs.ID]; ok {
			dbSess = &s
		}

		var plannedWake time.Time
		if i == 0 {
			if dbSess != nil && dbSess.WokeAt != nil {
				plannedWake = dbSess.WokeAt.Local()
			} else {
				h, m := parseHHMM(baseWakeTimes[0])
				plannedWake = today.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
			}
		} else {
			prev := views[i-1]
			var base time.Time
			if prev.ActualSleep != nil {
				base = prev.ActualSleep.Local()
			} else {
				base = prev.PlannedSleep
			}
			plannedWake = base.Add(nap)
		}

		var aw, as *time.Time
		if dbSess != nil {
			aw = dbSess.WokeAt
			as = dbSess.SleptAt
		}

		var actualDuration, durationClass string
		if aw != nil && as != nil {
			dur := as.Sub(*aw)
			actualDuration = formatDuration(dur)
			diff := dur - awake
			if diff < 0 {
				diff = -diff
			}
			diffMins := diff.Minutes()
			switch {
			case diffMins < 10:
				durationClass = "text-emerald-600"
			case diffMins < 20:
				durationClass = "text-amber-500"
			default:
				durationClass = "text-rose-500"
			}
		}

		var id int
		var comment, sleepEase, trainingQuality string
		var overtired, toiletPee, toiletPoop, toiletAccident bool
		var physicalActivity, mentalActivity, calmWinddown bool
		var ac *time.Time
		if dbSess != nil {
			id = dbSess.ID
			comment = dbSess.Comment
			sleepEase = dbSess.SleepEase
			overtired = dbSess.Overtired
			toiletPee = dbSess.ToiletPee
			toiletPoop = dbSess.ToiletPoop
			toiletAccident = dbSess.ToiletAccident
			trainingQuality = dbSess.TrainingQuality
			physicalActivity = dbSess.PhysicalActivity
			mentalActivity = dbSess.MentalActivity
			calmWinddown = dbSess.CalmWinddown
			ac = dbSess.CrateAt
		}

		var settleDuration string
		if ac != nil && as != nil {
			if d := as.Sub(*ac); d > 0 {
				settleDuration = formatDuration(d)
			}
		}

		views[i] = SessionView{
			ID:               id,
			Index:            i,
			Label:            rs.Label,
			Activities:       rs.Activities,
			PlannedWake:      plannedWake,
			PlannedSleep:     plannedWake.Add(awake),
			ActualWake:       aw,
			ActualCrate:      ac,
			ActualSleep:      as,
			IsPast:           as != nil,
			IsActive:         aw != nil && as == nil,
			IsFuture:         aw == nil,
			ActualDuration:   actualDuration,
			DurationClass:    durationClass,
			Comment:          comment,
			SleepEase:        sleepEase,
			Overtired:        overtired,
			ToiletPee:        toiletPee,
			ToiletPoop:       toiletPoop,
			ToiletAccident:   toiletAccident,
			TrainingQuality:  trainingQuality,
			PhysicalActivity: physicalActivity,
			MentalActivity:   mentalActivity,
			CalmWinddown:     calmWinddown,
			SettleDuration:   settleDuration,
		}
	}

	for i := 0; i < len(views)-1; i++ {
		if views[i].ActualSleep != nil && views[i+1].ActualWake != nil {
			d := views[i+1].ActualWake.Sub(*views[i].ActualSleep)
			if d > 0 {
				views[i].SleepDuration = formatDuration(d)
			}
		}
	}

	return views
}

// seedDefaultRoutine inserts the hardcoded daily plan into routine_sessions on
// first run. Does nothing if any sessions already exist.
func seedDefaultRoutine(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM routine_sessions`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for i, tmpl := range dailyPlan {
		acts := strings.Join(tmpl.Activities, "\n")
		if _, err := db.Exec(
			`INSERT INTO routine_sessions (position, label, activities) VALUES (?, ?, ?)`,
			i+1, tmpl.Label, acts,
		); err != nil {
			return err
		}
	}
	return nil
}

func parseHHMM(s string) (int, int) {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h, m
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
