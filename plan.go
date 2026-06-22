package main

import (
	"fmt"
	"time"
)

type SessionTemplate struct {
	Label      string
	Activities []string
	NapTarget  time.Duration // target nap duration after this session
}

// dailyPlan mirrors the Norwegian toller routine.
// NapTarget drives adaptive time calculation: each session's planned wake
// = previous session's actual_slept_at + previous NapTarget.
var dailyPlan = []SessionTemplate{
	{
		Label:      "Morning",
		Activities: []string{"Quick bathroom break outside", "Breakfast in playpen"},
		NapTarget:  80 * time.Minute,
	},
	{
		Label:      "Mid-morning",
		Activities: []string{"Environmental training in Oslo — carry or sit on bench (10–15 min)", "Calm walk home"},
		NapTarget:  95 * time.Minute,
	},
	{
		Label:      "Lunch",
		Activities: []string{"Lunch in playpen", "Calm cuddle", "Crate familiarisation (2–3 min, low-value food)"},
		NapTarget:  95 * time.Minute,
	},
	{
		Label:      "Afternoon",
		Activities: []string{"Mental training: sit / stay", "Solo time in playpen"},
		NapTarget:  80 * time.Minute,
	},
	{
		Label:      "Dinner",
		Activities: []string{"Dinner in playpen", "Calm wind-down"},
		NapTarget:  80 * time.Minute,
	},
	{
		Label:      "Evening",
		Activities: []string{"Evening snack in playpen", "Calm wind-down"},
		NapTarget:  90 * time.Minute,
	},
	{
		Label:      "Night outing",
		Activities: []string{"Last bathroom break — very calm, no play", "Cuddle on lap or floor only — no food, no games"},
		NapTarget:  0, // leads to night sleep, no automatic next session
	},
}

// baseWakeTimes are the default plan times when no actual session data exists.
var baseWakeTimes = []string{"09:00", "11:00", "13:15", "15:30", "17:30", "19:30", "21:40"}

// SessionView is the rendering model for one awake window in the schedule.
type SessionView struct {
	Index        int
	Label        string
	Activities   []string
	PlannedWake  time.Time
	PlannedSleep time.Time // PlannedWake + 40 min
	ActualWake   *time.Time
	ActualSleep  *time.Time
	IsPast       bool // slept_at is recorded
	IsActive     bool // woke_at set, slept_at nil
	IsFuture     bool // no actual data yet
}

// buildSchedule returns the full day's session list with planned times adjusted
// by actual data: if session N slept later than planned, all subsequent planned
// wake times shift forward by the same amount.
func buildSchedule(date string, dbSessions []DBSession) []SessionView {
	loc := time.Local
	today, _ := time.ParseInLocation("2006-01-02", date, loc)

	views := make([]SessionView, len(dailyPlan))

	for i, tmpl := range dailyPlan {
		var plannedWake time.Time

		if i == 0 {
			// First session: anchor to actual wake if known, else base plan.
			if len(dbSessions) > 0 && dbSessions[0].WokeAt != nil {
				plannedWake = dbSessions[0].WokeAt.Local()
			} else {
				h, m := parseHHMM(baseWakeTimes[0])
				plannedWake = today.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
			}
		} else {
			prev := views[i-1]
			var prevSleepBase time.Time
			if prev.ActualSleep != nil {
				prevSleepBase = prev.ActualSleep.Local()
			} else {
				prevSleepBase = prev.PlannedSleep
			}
			plannedWake = prevSleepBase.Add(dailyPlan[i-1].NapTarget)
		}

		var aw, as *time.Time
		if i < len(dbSessions) {
			aw = dbSessions[i].WokeAt
			as = dbSessions[i].SleptAt
		}

		views[i] = SessionView{
			Index:        i,
			Label:        tmpl.Label,
			Activities:   tmpl.Activities,
			PlannedWake:  plannedWake,
			PlannedSleep: plannedWake.Add(40 * time.Minute),
			ActualWake:   aw,
			ActualSleep:  as,
			IsPast:       as != nil,
			IsActive:     aw != nil && as == nil,
			IsFuture:     aw == nil,
		}
	}

	return views
}

func parseHHMM(s string) (int, int) {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h, m
}
