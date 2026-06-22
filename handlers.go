package main

import (
	"bytes"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed templates
var templateFS embed.FS

type App struct {
	db   *sql.DB
	tmpl *template.Template
}

type PageData struct {
	Phase          Phase
	Elapsed        string
	StartedAt      string
	Sessions       []SessionView
	Meals          []MealEntry
	NightLock      bool
	Config         *Config
	LastWokeAt     *time.Time
	LastSleptAt    *time.Time
	ShouldWindDown bool
}

type SettingsData struct {
	Config          *Config
	RoutineSessions []RoutineSession
}

type StatsData struct {
	Days   []DayStat
	Config *Config
}

func buildPageData(db *sql.DB) (*PageData, error) {
	now := time.Now()
	today := now.Format("2006-01-02")

	if err := closeStaleSession(db); err != nil {
		log.Printf("closeStaleSession: %v", err)
	}

	state, err := getState(db)
	if err != nil {
		return nil, fmt.Errorf("get state: %w", err)
	}

	cfg, err := getConfig(db)
	if err != nil {
		return nil, fmt.Errorf("get config: %w", err)
	}

	dbSessions, err := getSessionsForDate(db, today)
	if err != nil {
		return nil, fmt.Errorf("get sessions: %w", err)
	}

	routineSessions, err := getRoutineSessions(db)
	if err != nil {
		return nil, fmt.Errorf("get routine sessions: %w", err)
	}

	meals, err := getMeals(db, today)
	if err != nil {
		return nil, fmt.Errorf("get meals: %w", err)
	}

	elapsed := now.Sub(state.PhaseStartedAt.Local())
	nightLock := now.Hour() > 22 || (now.Hour() == 22 && now.Minute() >= 15)

	windDownThreshold := time.Duration(cfg.AwakeMinutes-15) * time.Minute
	shouldWindDown := state.Phase == PhaseActive && elapsed >= windDownThreshold

	var lastWokeAt, lastSleptAt *time.Time
	for i := len(dbSessions) - 1; i >= 0; i-- {
		if lastWokeAt == nil && dbSessions[i].WokeAt != nil {
			t := dbSessions[i].WokeAt.Local()
			lastWokeAt = &t
		}
		if lastSleptAt == nil && dbSessions[i].SleptAt != nil {
			t := dbSessions[i].SleptAt.Local()
			lastSleptAt = &t
		}
		if lastWokeAt != nil && lastSleptAt != nil {
			break
		}
	}

	return &PageData{
		Phase:          state.Phase,
		Elapsed:        formatDuration(elapsed),
		StartedAt:      state.PhaseStartedAt.Local().Format("15:04"),
		Sessions:       buildSchedule(today, dbSessions, routineSessions, cfg.AwakeMinutes, cfg.NapMinutes),
		Meals:          meals,
		NightLock:      nightLock,
		Config:         cfg,
		LastWokeAt:     lastWokeAt,
		LastSleptAt:    lastSleptAt,
		ShouldWindDown: shouldWindDown,
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
	}
	return template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
}

// ── page handlers ─────────────────────────────────────────────────────────────

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(a.db)
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
	a.renderStateFragment(w)
}

func (a *App) handlePostPhase(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	phase := Phase(r.FormValue("phase"))
	switch phase {
	case PhaseActive, PhaseSleeping:
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
	case PhaseSleeping:
		if err := logSleep(a.db); err != nil {
			log.Printf("logSleep: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := setPhase(a.db, phase); err != nil {
		log.Printf("setPhase: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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

func (a *App) handleAdjustWake(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	delta, err := strconv.Atoi(r.FormValue("delta"))
	if err != nil || delta < -120 || delta > 120 {
		http.Error(w, "invalid delta", http.StatusBadRequest)
		return
	}
	today := time.Now().Format("2006-01-02")
	if err := adjustWakeTime(a.db, today, delta); err != nil {
		log.Printf("adjustWakeTime: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

func (a *App) handleAdjustSleep(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	delta, err := strconv.Atoi(r.FormValue("delta"))
	if err != nil || delta < -120 || delta > 120 {
		http.Error(w, "invalid delta", http.StatusBadRequest)
		return
	}
	if err := adjustSleepTime(a.db, delta); err != nil {
		log.Printf("adjustSleepTime: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

// ── stats ─────────────────────────────────────────────────────────────────────

func (a *App) handleGetStats(w http.ResponseWriter, r *http.Request) {
	cfg, err := getConfig(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	days, err := getDayStats(a.db, cfg.AwakeMinutes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "stats-page", &StatsData{Days: days, Config: cfg}); err != nil {
		log.Printf("stats template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleSetSleepEase(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ease := r.FormValue("value")
	switch ease {
	case "easy", "ok", "hard":
	default:
		http.Error(w, "invalid value", http.StatusBadRequest)
		return
	}
	if err := setSleepEase(a.db, id, ease); err != nil {
		log.Printf("setSleepEase: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

func (a *App) handleToggleOvertired(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := toggleOvertired(a.db, id); err != nil {
		log.Printf("toggleOvertired: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
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
	if err := setSessionComment(a.db, id, comment); err != nil {
		log.Printf("setSessionComment: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
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

func (a *App) renderStateFragment(w http.ResponseWriter) {
	data, err := buildPageData(a.db)
	if err != nil {
		log.Printf("buildPageData: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := a.tmpl.ExecuteTemplate(&buf, "state", data); err != nil {
		log.Printf("ExecuteTemplate state: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (a *App) renderRoutineSessionsFrag(w http.ResponseWriter) {
	sessions, err := getRoutineSessions(a.db)
	if err != nil {
		log.Printf("getRoutineSessions: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := a.tmpl.ExecuteTemplate(&buf, "routine-sessions", sessions); err != nil {
		log.Printf("ExecuteTemplate routine-sessions: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}
