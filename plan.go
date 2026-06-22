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
	ActualDuration string // e.g. "45m" or "1h 5m" — only set for past sessions
	DurationClass  string // Tailwind color class based on delta from target
}

// buildSchedule constructs the day's session list with planned times adjusted
// by actual data. Each session's planned wake = previous session's actual_slept_at
// + napMins, cascading forward through the day.
func buildSchedule(date string, dbSessions []DBSession, routineSessions []RoutineSession, awakeMins, napMins int) []SessionView {
	loc := time.Local
	today, _ := time.ParseInLocation("2006-01-02", date, loc)
	awake := time.Duration(awakeMins) * time.Minute
	nap := time.Duration(napMins) * time.Minute

	views := make([]SessionView, len(routineSessions))

	for i, rs := range routineSessions {
		var plannedWake time.Time

		if i == 0 {
			if len(dbSessions) > 0 && dbSessions[0].WokeAt != nil {
				plannedWake = dbSessions[0].WokeAt.Local()
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
		if i < len(dbSessions) {
			aw = dbSessions[i].WokeAt
			as = dbSessions[i].SleptAt
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

		views[i] = SessionView{
			Index:          i,
			Label:          rs.Label,
			Activities:     rs.Activities,
			PlannedWake:    plannedWake,
			PlannedSleep:   plannedWake.Add(awake),
			ActualWake:     aw,
			ActualSleep:    as,
			IsPast:         as != nil,
			IsActive:       aw != nil && as == nil,
			IsFuture:       aw == nil,
			ActualDuration: actualDuration,
			DurationClass:  durationClass,
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
