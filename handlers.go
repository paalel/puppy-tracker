package main

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type App struct {
	db   *sql.DB
	tmpl *template.Template
}

type PageData struct {
	Phase          Phase
	Elapsed        string
	Sessions       []SessionView
	Meals          []MealEntry
	Config         *Config
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
}

type SettingsData struct {
	Config          *Config
	RoutineSessions []RoutineSession
}

type StatsData struct {
	Days       []DayStat
	Config     *Config
	Tab        string
	AwakeJSON  template.JS
	NapJSON    template.JS
	SettleJSON template.JS
}

func buildPageData(db *sql.DB, date string) (*PageData, error) {
	now := time.Now()
	today := now.Format("2006-01-02")
	isToday := date == today

	d, _ := time.Parse("2006-01-02", date)
	prevDate := d.AddDate(0, 0, -1).Format("2006-01-02")
	var nextDate string
	if !isToday {
		if next := d.AddDate(0, 0, 1).Format("2006-01-02"); next <= today {
			nextDate = next
		}
	}

	cfg, err := getConfig(db)
	if err != nil {
		return nil, fmt.Errorf("get config: %w", err)
	}

	dbSessions, err := getSessionsForDate(db, date)
	if err != nil {
		return nil, fmt.Errorf("get sessions: %w", err)
	}

	routineSessions, err := getRoutineSessions(db)
	if err != nil {
		return nil, fmt.Errorf("get routine sessions: %w", err)
	}

	meals, err := getMeals(db, date)
	if err != nil {
		return nil, fmt.Errorf("get meals: %w", err)
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
		shouldWindDown = phase == PhaseActive && e >= time.Duration(cfg.AwakeMinutes-15)*time.Minute

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

	return &PageData{
		Phase:          phase,
		Elapsed:        elapsed,
		Sessions:       buildSchedule(date, dbSessions, routineSessions, cfg),
		Meals:          meals,
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
	}, nil
}

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"isPhase": func(p Phase, name string) bool {
			return p == Phase(name)
		},
		"isSelected": func(current MealAmount, checking string) bool {
			return current == MealAmount(checking)
		},
		"isPastDeadline": func(deadline string) bool {
			now := time.Now()
			t, err := time.Parse("15:04", deadline)
			if err != nil {
				return false
			}
			dl := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			return now.After(dl)
		},
		"fmtTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Local().Format("15:04")
		},
		"fmtPlan": func(t time.Time) string {
			return t.Format("15:04")
		},
		"currentSession": func(sessions []SessionView) *SessionView {
			for i := range sessions {
				if sessions[i].IsActive {
					return &sessions[i]
				}
			}
			return nil
		},
		"nextSession": func(sessions []SessionView) *SessionView {
			for i := range sessions {
				if sessions[i].IsFuture {
					return &sessions[i]
				}
			}
			return nil
		},
		"joinActivities": joinActivities,
		"dayLabel": func(date string) string {
			today := time.Now().Format("2006-01-02")
			yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
			switch date {
			case today:
				return "Today"
			case yesterday:
				return "Yesterday"
			default:
				if t, err := time.Parse("2006-01-02", date); err == nil {
					return t.Format("Mon Jan 2")
				}
				return date
			}
		},
		"awakeClass": func(avgMins, targetMins int) string {
			diff := avgMins - targetMins
			if diff < 0 {
				diff = -diff
			}
			switch {
			case diff < 10:
				return "text-emerald-600"
			case diff < 20:
				return "text-amber-500"
			default:
				return "text-rose-500"
			}
		},
		"dict": func(pairs ...any) (map[string]any, error) {
			if len(pairs)%2 != 0 {
				return nil, fmt.Errorf("dict requires an even number of arguments")
			}
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs); i += 2 {
				key, ok := pairs[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				m[key] = pairs[i+1]
			}
			return m, nil
		},
	}
	return template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
}

// ── page handlers ─────────────────────────────────────────────────────────────

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := closeStaleSession(a.db); err != nil {
		log.Printf("closeStaleSession: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	date := r.URL.Query().Get("date")
	if date == "" || date > today {
		date = today
	}
	data, err := buildPageData(a.db, date)
	if err != nil {
		log.Printf("handleIndex buildPageData: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("handleIndex template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := getConfig(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rs, err := getRoutineSessions(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "settings-page", &SettingsData{Config: cfg, RoutineSessions: rs}); err != nil {
		log.Printf("settings template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handlePostSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("puppy_name"))
	awakeMins, _ := strconv.Atoi(r.FormValue("awake_minutes"))
	napMins, _ := strconv.Atoi(r.FormValue("nap_minutes"))

	// Fall back to existing config rather than hardcoding defaults
	current, err := getConfig(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if name == "" {
		name = current.PuppyName
	}
	if awakeMins <= 0 {
		awakeMins = current.AwakeMinutes
	}
	if napMins <= 0 {
		napMins = current.NapMinutes
	}

	cfg := &Config{PuppyName: name, AwakeMinutes: awakeMins, NapMinutes: napMins}
	if err := saveConfig(a.db, cfg); err != nil {
		log.Printf("saveConfig: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// ── API: state ────────────────────────────────────────────────────────────────

func (a *App) handleGetState(w http.ResponseWriter, r *http.Request) {
	if err := closeStaleSession(a.db); err != nil {
		log.Printf("closeStaleSession: %v", err)
	}
	a.renderStateFragment(w)
}

func (a *App) handlePostPhase(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	phase := Phase(r.FormValue("phase"))
	switch phase {
	case PhaseActive, PhaseCrating, PhaseSleeping:
	default:
		http.Error(w, "invalid phase", http.StatusBadRequest)
		return
	}

	today := time.Now().Format("2006-01-02")

	switch phase {
	case PhaseActive:
		if err := logWake(a.db, today); err != nil {
			log.Printf("logWake: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case PhaseCrating:
		if err := logCrate(a.db); err != nil {
			log.Printf("logCrate: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case PhaseSleeping:
		if err := logSleep(a.db); err != nil {
			log.Printf("logSleep: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	a.renderStateFragment(w)
}

func (a *App) handlePostMeal(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	mealType := MealType(r.FormValue("meal"))
	amount := MealAmount(r.FormValue("amount"))
	today := time.Now().Format("2006-01-02")

	switch mealType {
	case MealBreakfast, MealLunch, MealDinner:
	default:
		http.Error(w, "invalid meal", http.StatusBadRequest)
		return
	}
	switch amount {
	case AmountNothing, AmountTooLittle, AmountPrettyGood, AmountFullMeal:
	default:
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}

	if err := setMeal(a.db, today, mealType, amount); err != nil {
		log.Printf("setMeal: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

func (a *App) handleAdjustSessionTime(column string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		delta, err := strconv.Atoi(r.FormValue("delta"))
		if err != nil || delta < -120 || delta > 120 {
			http.Error(w, "invalid delta", http.StatusBadRequest)
			return
		}
		if err := adjustLatestSessionTime(a.db, column, delta); err != nil {
			log.Printf("adjustLatestSessionTime %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.renderStateFragment(w)
	}
}

// ── stats ─────────────────────────────────────────────────────────────────────

func (a *App) handleGetStats(w http.ResponseWriter, r *http.Request) {
	cfg, err := getConfig(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	days, err := getDayStats(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab != "graph" {
		tab = "log"
	}
	sd := &StatsData{Days: days, Config: cfg, Tab: tab}
	if tab == "graph" {
		series, err := getSessionSeries(a.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mustJSON := func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		}
		sd.AwakeJSON = mustJSON(series.Awake)
		sd.NapJSON = mustJSON(series.Nap)
		sd.SettleJSON = mustJSON(series.Settle)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "stats-page", sd); err != nil {
		log.Printf("stats template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleSetSessionEnum(column string, allowed ...string) http.HandlerFunc {
	set := make(map[string]bool, len(allowed))
	for _, v := range allowed {
		set[v] = true
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		value := r.FormValue("value")
		if !set[value] {
			http.Error(w, "invalid value", http.StatusBadRequest)
			return
		}
		if err := setSessionString(a.db, id, column, value); err != nil {
			log.Printf("setSessionString %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.renderStateFragment(w)
	}
}

func (a *App) handleToggleSessionBool(column string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := toggleSessionBool(a.db, id, column); err != nil {
			log.Printf("toggleSessionBool %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.renderStateFragment(w)
	}
}

func (a *App) handleSetSessionComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	comment := strings.TrimSpace(r.FormValue("comment"))
	if err := setSessionString(a.db, id, "comment", comment); err != nil {
		log.Printf("setSessionString comment: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

func (a *App) handleSetSessionTime(column string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		t, err := time.ParseInLocation("15:04", r.FormValue("time"), time.Local)
		if err != nil {
			http.Error(w, "invalid time", http.StatusBadRequest)
			return
		}
		if err := setSessionTime(a.db, id, column, t); err != nil {
			log.Printf("setSessionTime %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.renderStateFragment(w)
	}
}

// ── API: routine sessions ─────────────────────────────────────────────────────

func (a *App) handleCreateRoutineSession(w http.ResponseWriter, r *http.Request) {
	if err := createRoutineSession(a.db); err != nil {
		log.Printf("createRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderRoutineSessionsFrag(w)
}

func (a *App) handleUpdateRoutineSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := updateRoutineSession(a.db, id, r.FormValue("label"), r.FormValue("activities")); err != nil {
		log.Printf("updateRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderRoutineSessionsFrag(w)
}

func (a *App) handleDeleteRoutineSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := deleteRoutineSession(a.db, id); err != nil {
		log.Printf("deleteRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderRoutineSessionsFrag(w)
}

func (a *App) handleMoveRoutineSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	dir := 1
	switch r.PathValue("dir") {
	case "up":
		dir = -1
	case "down":
		// dir = 1, already set
	default:
		http.Error(w, "invalid dir", http.StatusBadRequest)
		return
	}
	if err := moveRoutineSession(a.db, id, dir); err != nil {
		log.Printf("moveRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderRoutineSessionsFrag(w)
}

// ── fragment renderers ────────────────────────────────────────────────────────

func (a *App) renderFragment(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := a.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("renderFragment %s: %v", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

func (a *App) renderStateFragment(w http.ResponseWriter) {
	data, err := buildPageData(a.db, time.Now().Format("2006-01-02"))
	if err != nil {
		log.Printf("buildPageData: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderFragment(w, "state", data)
}

func (a *App) renderRoutineSessionsFrag(w http.ResponseWriter) {
	sessions, err := getRoutineSessions(a.db)
	if err != nil {
		log.Printf("getRoutineSessions: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderFragment(w, "routine-sessions", sessions)
}
