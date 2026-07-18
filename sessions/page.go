package sessions

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"puppy/config"
	"puppy/routine"
	"puppy/store"
)

// wakeDate returns the rollover-aware session date for the current moment.
func wakeDate() string { return store.RolloverDate() }

type PageData struct {
	Phase          Phase
	Elapsed        string
	Sessions       []SessionView
	Config         *config.Config
	LastWokeAt     *time.Time
	LastSleptAt    *time.Time
	ShouldWindDown bool
	LastCrateAt    *time.Time
	IsLocal        bool
	DBPath         string
	Date           string
	IsToday        bool
	IsNight        bool
	PrevDate       string
	NextDate       string
	PoopStatus     *PoopStatus
	PoopLikelihood float64
	PoopLo         float64
	PoopHi         float64
}

func buildPageData(db *sql.DB, date string, pred *PoopPredictor) (*PageData, error) {
	now := time.Now()
	today := store.RolloverDate()
	isToday := date == today

	d, _ := store.ParseDate(date)
	prevDate := store.FormatDate(d.AddDate(0, 0, -1))
	var nextDate string
	if !isToday {
		if next := store.FormatDate(d.AddDate(0, 0, 1)); next <= today {
			nextDate = next
		}
	}

	cfg, err := config.Get(db)
	if err != nil {
		return nil, fmt.Errorf("get config: %w", err)
	}

	dbSessions, err := getSessionsForDate(db, date)
	if err != nil {
		return nil, fmt.Errorf("get sessions: %w", err)
	}

	routineSessions, err := routine.GetAll(db)
	if err != nil {
		return nil, fmt.Errorf("get routine sessions: %w", err)
	}

	var phase Phase
	var elapsed string
	var shouldWindDown bool
	var lastWokeAt, lastCrateAt, lastSleptAt *time.Time

	if isToday {
		state, err := getState(db)
		if err != nil {
			return nil, fmt.Errorf("get state: %w", err)
		}
		e := now.Sub(state.PhaseStartedAt.Local())
		elapsed = formatDuration(e)
		phase = state.Phase
		shouldWindDown = phase == PhaseActive && e >= time.Duration(cfg.WindDownMinutes)*time.Minute

		for i := len(dbSessions) - 1; i >= 0; i-- {
			if lastWokeAt == nil && dbSessions[i].WokeAt != nil {
				t := dbSessions[i].WokeAt.Local()
				lastWokeAt = &t
			}
			if lastCrateAt == nil && dbSessions[i].CrateAt != nil {
				t := dbSessions[i].CrateAt.Local()
				lastCrateAt = &t
			}
			if lastSleptAt == nil && dbSessions[i].SleptAt != nil {
				t := dbSessions[i].SleptAt.Local()
				lastSleptAt = &t
			}
			if lastWokeAt != nil && lastCrateAt != nil && lastSleptAt != nil {
				break
			}
		}
	}

	ps, err := getPoopStatus(db)
	if err != nil {
		return nil, fmt.Errorf("poop status: %w", err)
	}

	hr := now.Local().Hour()
	isNight := hr >= 21 || hr < store.WakeRolloverHour

	views := buildSchedule(date, dbSessions, routineSessions, cfg)

	if pred != nil && isToday {
		hoursSincePoop, _ := getHoursSinceLastPoop(db)
		if hoursSincePoop >= 0 {
			cycleHours := float64(cfg.AwakeMinutes+cfg.NapMinutes) / 60.0
			futureOffset := 0
			for i := range views {
				var localHour int
				if views[i].ActualWake != nil {
					localHour = views[i].ActualWake.Local().Hour()
				} else {
					localHour = views[i].PlannedWake.Local().Hour()
				}
				var mid, lo, hi float64
				switch {
				case views[i].IsActive:
					mid, lo, hi = pred.Predict(localHour, hoursSincePoop)
				case views[i].IsFuture:
					futureOffset++
					mid, lo, hi = pred.Predict(localHour, hoursSincePoop+float64(futureOffset)*cycleHours)
				}
				views[i].PoopLikelihood = mid
				views[i].PoopLo = lo
				views[i].PoopHi = hi
			}
		}
	}

	var poopLikelihood, poopLo, poopHi float64
	for _, v := range views {
		if (v.IsActive || v.IsFuture) && v.PoopLikelihood > 0 {
			poopLikelihood = v.PoopLikelihood
			poopLo = v.PoopLo
			poopHi = v.PoopHi
			break
		}
	}

	return &PageData{
		Phase:          phase,
		Elapsed:        elapsed,
		Sessions:       views,
		Config:         cfg,
		LastWokeAt:     lastWokeAt,
		LastCrateAt:    lastCrateAt,
		LastSleptAt:    lastSleptAt,
		ShouldWindDown: shouldWindDown,
		IsLocal:        os.Getenv("FLY_APP_NAME") == "",
		DBPath:         os.Getenv("DATABASE_PATH"),
		Date:           date,
		IsToday:        isToday,
		IsNight:        isNight,
		PrevDate:       prevDate,
		NextDate:       nextDate,
		PoopStatus:     ps,
		PoopLikelihood: poopLikelihood,
		PoopLo:         poopLo,
		PoopHi:         poopHi,
	}, nil
}

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

		// PlannedSleep is the expected sleep time based on when the puppy actually
		// woke up (if known), so the cascade to future sessions stays accurate even
		// when a session ran long (e.g. vet visit).
		plannedSleep := plannedWake.Add(awake)
		if aw != nil {
			plannedSleep = aw.Local().Add(awake)
		}

		views[i] = SessionView{
			ID:                    id,
			Index:                 i,
			Position:              rs.Position,
			Label:                 rs.Label,
			Activities:            rs.Activities,
			PlannedWake:           plannedWake,
			PlannedSleep:          plannedSleep,
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
