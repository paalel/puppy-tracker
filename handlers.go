package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

type App struct {
	db   *sql.DB
	tmpl *template.Template
}

type PageData struct {
	Phase     Phase
	Elapsed   string
	StartedAt string
	Sessions  []SessionView
	Meals     []MealEntry
	NightLock bool
}

func buildPageData(db *sql.DB) (*PageData, error) {
	now := time.Now()
	today := now.Format("2006-01-02")

	state, err := getState(db)
	if err != nil {
		return nil, fmt.Errorf("get state: %w", err)
	}

	dbSessions, err := getSessionsForDate(db, today)
	if err != nil {
		return nil, fmt.Errorf("get sessions: %w", err)
	}

	meals, err := getMeals(db, today)
	if err != nil {
		return nil, fmt.Errorf("get meals: %w", err)
	}

	elapsed := now.Sub(state.PhaseStartedAt.Local())
	nightLock := now.Hour() > 22 || (now.Hour() == 22 && now.Minute() >= 15)

	return &PageData{
		Phase:     state.Phase,
		Elapsed:   formatElapsed(elapsed),
		StartedAt: state.PhaseStartedAt.Local().Format("15:04"),
		Sessions:  buildSchedule(today, dbSessions),
		Meals:     meals,
		NightLock: nightLock,
	}, nil
}

func formatElapsed(d time.Duration) string {
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
		// fmtTime formats an optional *time.Time for display
		"fmtTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Local().Format("15:04")
		},
		// fmtPlan formats a non-optional planned time
		"fmtPlan": func(t time.Time) string {
			return t.Format("15:04")
		},
		// currentSession returns the session currently awake, or nil
		"currentSession": func(sessions []SessionView) *SessionView {
			for i := range sessions {
				if sessions[i].IsActive {
					return &sessions[i]
				}
			}
			return nil
		},
		// nextSession returns the first future session (no actual wake time yet)
		"nextSession": func(sessions []SessionView) *SessionView {
			for i := range sessions {
				if sessions[i].IsFuture {
					return &sessions[i]
				}
			}
			return nil
		},
	}
	return template.New("").Funcs(funcs).ParseGlob("templates/*.html")
}

// ── handlers ──────────────────────────────────────────────────────────────────

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleGetState serves the state fragment for polling clients.
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
	case PhaseActive, PhaseWindDown, PhaseSleeping:
	default:
		http.Error(w, "invalid phase", http.StatusBadRequest)
		return
	}

	today := time.Now().Format("2006-01-02")

	// Log session events on the transitions that matter
	switch phase {
	case PhaseActive:
		if err := logWake(a.db, today); err != nil {
			log.Printf("logWake: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case PhaseSleeping:
		if err := logSleep(a.db, today); err != nil {
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

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
