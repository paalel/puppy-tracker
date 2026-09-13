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

	var views []SessionView
	if isToday {
		views = buildSchedule(date, dbSessions, routineSessions, cfg)
	} else {
		views = buildPastSchedule(dbSessions, routineSessions)
	}

	// Show how long she slept before the current awake window. linkNaps only
	// spans same-day sessions, so the first wake of the day (after the overnight
	// sleep) would otherwise be blank — fill it from the previous slept_at.
	if isToday {
		for i := range views {
			if !views[i].IsActive || views[i].SleepDuration != "" || views[i].ActualWake == nil {
				continue
			}
			prev, err := getSleptAtBefore(db, *views[i].ActualWake)
			if err != nil {
				return nil, fmt.Errorf("sleep before active: %w", err)
			}
			if prev != nil {
				if d := views[i].ActualWake.Sub(*prev); d > 0 {
					views[i].SleepDuration = formatDuration(d)
				}
			}
		}
	}

	if pred != nil && isToday {
		hoursSincePoop, _ := getHoursSinceLastPoop(db)
		if hoursSincePoop >= 0 {
			cycleHours := float64(cfg.AwakeMinutes+cfg.NapMinutes) / 60.0
			currentHour := now.Local().Hour()
			futureOffset := 0
			for i := range views {
				var mid, lo, hi float64
				switch {
				case views[i].IsActive:
					// Use live clock hour so probability updates each hour as time passes.
					mid, lo, hi = pred.Predict(currentHour, hoursSincePoop)
				case views[i].IsFuture:
					futureOffset++
					plannedHour := views[i].PlannedWake.Local().Hour()
					mid, lo, hi = pred.Predict(plannedHour, hoursSincePoop+float64(futureOffset)*cycleHours)
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
		PrevDate:       prevDate,
		NextDate:       nextDate,
		PoopStatus:     ps,
		PoopLikelihood: poopLikelihood,
		PoopLo:         poopLo,
		PoopHi:         poopHi,
	}, nil
}

// buildPastSchedule renders past days directly from DB sessions, ordered by
// woke_at. Uses the current routine only for labels; sessions whose routine
// slot was deleted still appear with a generic label.
func buildPastSchedule(dbSessions []dbSession, routineSessions []routine.RoutineSession) []SessionView {
	labelByID := make(map[int]string, len(routineSessions))
	for _, rs := range routineSessions {
		labelByID[rs.ID] = rs.Label
	}

	views := make([]SessionView, 0, len(dbSessions))
	for i, s := range dbSessions {
		v := sessionViewFromDB(s)
		v.Index = i
		v.Label = "Session"
		if s.RoutineSessionID != nil {
			if l, ok := labelByID[*s.RoutineSessionID]; ok {
				v.Label = l
			}
		}
		// Past days grey the duration rather than colour-coding against target.
		if v.ActualDuration != "" {
			v.DurationClass = "text-stone-400"
		}
		views = append(views, v)
	}

	linkNaps(views)
	return views
}

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

	// Track which DB sessions are consumed by a routine slot so we can
	// append any extras at the end.
	consumedDBIDs := make(map[int]bool, len(dbSessions))

	views := make([]SessionView, len(routineSessions))

	for i, rs := range routineSessions {
		var v SessionView
		if s, ok := dbByRoutineID[rs.ID]; ok {
			v = sessionViewFromDB(s)
			consumedDBIDs[s.ID] = true
		}
		v.Index = i
		v.Position = rs.Position
		v.Label = rs.Label
		v.Activities = rs.Activities
		// A routine slot with no matching row yet is a future session; the zero
		// value leaves IsFuture false, so classify explicitly here.
		v.IsPast = v.ActualSleep != nil
		v.IsActive = v.ActualWake != nil && v.ActualSleep == nil
		v.IsFuture = v.ActualWake == nil

		// Planned wake baseline: first slot from the configured wake time, later
		// slots cascade from the previous slot's actual (or, if unknown, planned)
		// sleep, so a long-running session pushes the rest of the day back.
		var plannedWake time.Time
		if i == 0 {
			h, m := parseHHMM(cfg.FirstWakeTime)
			plannedWake = today.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
		} else {
			prev := views[i-1]
			base := prev.PlannedSleep
			if prev.ActualSleep != nil {
				base = *prev.ActualSleep
			}
			plannedWake = base.Add(nap)
		}
		// Once she's actually awake, the real wake time drives both ends.
		plannedSleep := plannedWake.Add(awake)
		if v.ActualWake != nil {
			plannedWake = *v.ActualWake
			plannedSleep = v.ActualWake.Add(awake)
		}
		v.PlannedWake = plannedWake
		v.PlannedSleep = plannedSleep

		if v.ActualWake != nil && v.ActualSleep != nil {
			v.DurationClass = durationClass(v.ActualSleep.Sub(*v.ActualWake), awake)
		}
		views[i] = v
	}

	// Append any DB sessions that weren't matched to a routine slot.
	for _, s := range dbSessions {
		if consumedDBIDs[s.ID] {
			continue
		}
		v := sessionViewFromDB(s)
		v.Index = len(views)
		if v.ActualWake != nil && v.ActualSleep != nil {
			v.DurationClass = durationClass(v.ActualSleep.Sub(*v.ActualWake), awake)
		}
		views = append(views, v)
	}

	linkNaps(views)
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

// localize returns t in local time, preserving nil.
func localize(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	lt := t.Local()
	return &lt
}

// durationClass colour-codes how far an awake window's actual length strays from
// the target: green when close, amber when off, rose when far.
func durationClass(actual, target time.Duration) string {
	diff := actual - target
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff.Minutes() < 10:
		return "text-emerald-600"
	case diff.Minutes() < 20:
		return "text-amber-500"
	default:
		return "text-rose-500"
	}
}

// settleDur is the crate→sleep duration, or "" if either bound is missing/invalid.
func settleDur(crate, sleep *time.Time) string {
	if crate != nil && sleep != nil {
		if d := sleep.Sub(*crate); d > 0 {
			return formatDuration(d)
		}
	}
	return ""
}

// linkNaps fills each session's SleepDuration from the gap between the previous
// session's sleep and this session's wake.
func linkNaps(views []SessionView) {
	for i := 0; i < len(views)-1; i++ {
		if views[i].ActualSleep != nil && views[i+1].ActualWake != nil {
			if d := views[i+1].ActualWake.Sub(*views[i].ActualSleep); d > 0 {
				views[i+1].SleepDuration = formatDuration(d)
			}
		}
	}
}

// sessionViewFromDB fills the fields of a SessionView that derive purely from a
// stored row: localized times, phase classification, pass-through flags, and the
// actual awake + settle durations. Schedule-specific fields (Label, Index, planned
// times, DurationClass) are left for the caller.
func sessionViewFromDB(s dbSession) SessionView {
	aw, ac, as := localize(s.WokeAt), localize(s.CrateAt), localize(s.SleptAt)
	v := SessionView{
		ID:                    s.ID,
		ActualWake:            aw,
		ActualCrate:           ac,
		ActualSleep:           as,
		IsPast:                as != nil,
		IsActive:              aw != nil && as == nil,
		IsFuture:              aw == nil,
		SettleDuration:        settleDur(ac, as),
		Comment:               s.Comment,
		SleepEase:             s.SleepEase,
		Overtired:             s.Overtired,
		ToiletPee:             s.ToiletPee,
		ToiletPoop:            s.ToiletPoop,
		ToiletAccident:        s.ToiletAccident,
		TrainingQuality:       s.TrainingQuality,
		PhysicalActivity:      s.PhysicalActivity,
		MentalActivity:        s.MentalActivity,
		CalmWinddown:          s.CalmWinddown,
		EnvironmentalActivity: s.EnvironmentalActivity,
		Excluded:              s.Excluded,
		Alone:                 s.Alone,
		AloneSlept:            s.AloneSlept,
		AloneBehaviour:        s.AloneBehaviour,
		AloneLocation:         s.AloneLocation,
		AloneDestroyed:        s.AloneDestroyed,
	}
	if aw != nil && as != nil {
		v.ActualDuration = formatDuration(as.Sub(*aw))
	}
	return v
}
