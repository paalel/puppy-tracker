package sessions

import (
	"fmt"
	"time"

	"puppy/config"
	"puppy/routine"
)

var baseWakeTimes = []string{"09:00"}

// buildSchedule constructs the day's session list with planned times adjusted
// by actual data. Each session's planned wake = previous session's actual_slept_at
// + napMins, cascading forward through the day.
func buildSchedule(date string, dbSessions []dbSession, routineSessions []routine.RoutineSession, cfg *config.Config) []SessionView {
	loc := time.Local
	today, _ := time.ParseInLocation("2006-01-02", date, loc)
	awake := time.Duration(cfg.AwakeMinutes) * time.Minute
	nap := time.Duration(cfg.NapMinutes) * time.Minute

	dbByRoutineID := make(map[int]dbSession, len(dbSessions))
	for _, s := range dbSessions {
		if s.RoutineSessionID != nil {
			dbByRoutineID[*s.RoutineSessionID] = s
		}
	}

	views := make([]SessionView, len(routineSessions))

	for i, rs := range routineSessions {
		var dbSess *dbSession
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
		var physicalActivity, mentalActivity, calmWinddown, environmentalActivity, excluded bool
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
			environmentalActivity = dbSess.EnvironmentalActivity
			excluded = dbSess.Excluded
			ac = dbSess.CrateAt
		}

		var settleDuration string
		if ac != nil && as != nil {
			if d := as.Sub(*ac); d > 0 {
				settleDuration = formatDuration(d)
			}
		}

		views[i] = SessionView{
			ID:                    id,
			Index:                 i,
			Label:                 rs.Label,
			Activities:            rs.Activities,
			PlannedWake:           plannedWake,
			PlannedSleep:          plannedWake.Add(awake),
			ActualWake:            aw,
			ActualCrate:           ac,
			ActualSleep:           as,
			IsPast:                as != nil,
			IsActive:              aw != nil && as == nil,
			IsFuture:              aw == nil,
			ActualDuration:        actualDuration,
			DurationClass:         durationClass,
			Comment:               comment,
			SleepEase:             sleepEase,
			Overtired:             overtired,
			ToiletPee:             toiletPee,
			ToiletPoop:            toiletPoop,
			ToiletAccident:        toiletAccident,
			TrainingQuality:       trainingQuality,
			PhysicalActivity:      physicalActivity,
			MentalActivity:        mentalActivity,
			CalmWinddown:          calmWinddown,
			EnvironmentalActivity: environmentalActivity,
			SettleDuration:        settleDuration,
			Excluded:              excluded,
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
