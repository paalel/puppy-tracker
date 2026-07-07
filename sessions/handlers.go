package sessions

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"puppy/config"
	"puppy/routine"
)

type Handler struct {
	db   *sql.DB
	tmpl *template.Template
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{db: db, tmpl: tmpl}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.handleIndex)
	mux.HandleFunc("GET /api/state", h.handleGetState)
	mux.HandleFunc("POST /api/phase", h.handlePostPhase)
	mux.HandleFunc("POST /api/phase/undo", h.handleUndoPhase)
	mux.HandleFunc("POST /api/wake-adjust", h.handleAdjustSessionTime("woke_at"))
	mux.HandleFunc("POST /api/crate-adjust", h.handleAdjustSessionTime("crate_at"))
	mux.HandleFunc("POST /api/sleep-adjust", h.handleAdjustSessionTime("slept_at"))
	mux.HandleFunc("POST /api/session/{id}/comment", h.handleSetSessionComment)
	mux.HandleFunc("POST /api/session/{id}/sleep-ease", h.handleSetSessionEnum("sleep_ease", "easy", "ok", "hard"))
	mux.HandleFunc("POST /api/session/{id}/overtired", h.handleToggleSessionBool("overtired"))
	mux.HandleFunc("POST /api/session/{id}/wake-time", h.handleSetSessionTime("woke_at"))
	mux.HandleFunc("POST /api/session/{id}/crate-time", h.handleSetSessionTime("crate_at"))
	mux.HandleFunc("POST /api/session/{id}/sleep-time", h.handleSetSessionTime("slept_at"))
	mux.HandleFunc("POST /api/session/{id}/toilet", h.handleToggleToilet)
	mux.HandleFunc("POST /api/session/{id}/training-quality", h.handleSetSessionEnum("training_quality", "sharp", "ok", "distracted"))
	mux.HandleFunc("POST /api/session/{id}/physical-activity", h.handleToggleSessionBool("physical_activity"))
	mux.HandleFunc("POST /api/session/{id}/mental-activity", h.handleToggleSessionBool("mental_activity"))
	mux.HandleFunc("POST /api/session/{id}/calm-winddown", h.handleToggleSessionBool("calm_winddown"))
	mux.HandleFunc("POST /api/session/{id}/environmental-activity", h.handleToggleSessionBool("environmental_activity"))
	mux.HandleFunc("POST /api/session/{id}/excluded", h.handleToggleSessionBool("excluded"))
	mux.HandleFunc("POST /api/night-toilet", h.handleNightToilet)
}

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
	isNight := hr >= 21 || hr < 4

	return &PageData{
		Phase:          phase,
		Elapsed:        elapsed,
		Sessions:       buildSchedule(date, dbSessions, routineSessions, cfg),
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
	}, nil
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := closeStaleSession(h.db); err != nil {
		log.Printf("closeStaleSession: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	date := r.URL.Query().Get("date")
	if date == "" || date > today {
		date = today
	}
	data, err := buildPageData(h.db, date)
	if err != nil {
		log.Printf("handleIndex buildPageData: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("handleIndex template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (h *Handler) handleGetState(w http.ResponseWriter, r *http.Request) {
	if err := closeStaleSession(h.db); err != nil {
		log.Printf("closeStaleSession: %v", err)
	}
	h.renderStateFragment(w)
}

func (h *Handler) handlePostPhase(w http.ResponseWriter, r *http.Request) {
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

	// Wakes before 04:00 local belong to the previous calendar day
	now := time.Now().Local()
	today := now.Format("2006-01-02")
	if now.Hour() < 4 {
		today = now.AddDate(0, 0, -1).Format("2006-01-02")
	}

	switch phase {
	case PhaseActive:
		if err := logWake(h.db, today); err != nil {
			log.Printf("logWake: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case PhaseCrating:
		if err := logCrate(h.db); err != nil {
			log.Printf("logCrate: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case PhaseSleeping:
		if err := logSleep(h.db); err != nil {
			log.Printf("logSleep: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	h.renderStateFragment(w)
}

func (h *Handler) handleUndoPhase(w http.ResponseWriter, r *http.Request) {
	if err := undoPhase(h.db); err != nil {
		log.Printf("handleUndoPhase: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderStateFragment(w)
}

func (h *Handler) handleAdjustSessionTime(column string) http.HandlerFunc {
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
		if err := adjustLatestSessionTime(h.db, column, delta); err != nil {
			log.Printf("adjustLatestSessionTime %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.renderStateFragment(w)
	}
}

func (h *Handler) handleSetSessionEnum(column string, allowed ...string) http.HandlerFunc {
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
		if err := setSessionString(h.db, id, column, value); err != nil {
			log.Printf("setSessionString %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.renderStateFragment(w)
	}
}

func (h *Handler) handleToggleSessionBool(column string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := toggleSessionBool(h.db, id, column); err != nil {
			log.Printf("toggleSessionBool %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.renderStateFragment(w)
	}
}

func (h *Handler) handleSetSessionComment(w http.ResponseWriter, r *http.Request) {
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
	if err := setSessionString(h.db, id, "comment", comment); err != nil {
		log.Printf("setSessionString comment: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderStateFragment(w)
}

func (h *Handler) handleSetSessionTime(column string) http.HandlerFunc {
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
		if err := setSessionTime(h.db, id, column, t); err != nil {
			log.Printf("setSessionTime %s: %v", column, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		today := time.Now().Format("2006-01-02")
		if sessionDate, err := getSessionDate(h.db, id); err == nil && sessionDate != today {
			http.Redirect(w, r, "/?date="+sessionDate, http.StatusSeeOther)
			return
		}
		h.renderStateFragment(w)
	}
}

func (h *Handler) handleToggleToilet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := toggleToilet(h.db, id, r.FormValue("value")); err != nil {
		log.Printf("handleToggleToilet: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderStateFragment(w)
}

func (h *Handler) handleNightToilet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	value := r.FormValue("value")
	allowed := map[string]bool{"pee": true, "poop": true, "accident": true}
	if !allowed[value] {
		http.Error(w, "invalid value", http.StatusBadRequest)
		return
	}
	if err := logNightToilet(h.db, value); err != nil {
		log.Printf("logNightToilet: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderStateFragment(w)
}

func (h *Handler) renderFragment(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("renderFragment %s: %v", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

func (h *Handler) renderStateFragment(w http.ResponseWriter) {
	data, err := buildPageData(h.db, time.Now().Format("2006-01-02"))
	if err != nil {
		log.Printf("buildPageData: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFragment(w, "state", data)
}
