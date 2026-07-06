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
	Config         *Config
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

type SettingsData struct {
	Config          *Config
	RoutineSessions []RoutineSession
}

type StatsData struct {
	Days             []DayStat
	Config           *Config
	Tab              string
	AwakeJSON        template.JS
	NapJSON          template.JS
	SettleEasyJSON   template.JS
	SettleOkJSON     template.JS
	SettleHardJSON   template.JS
	SettleNoneJSON   template.JS
	AccidentFreeDays int
	BucketJSON       template.JS
	KDEJSON          template.JS
	BetaJSON         template.JS
	TotalPoops       int
	TotalWakes       int
	// Sleep tab
	TotalSleepJSON template.JS
	NapByTimeJSON  template.JS
}

type dailyHours struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

func sendNtfyNotification(topic, title, message string) error {
	req, err := http.NewRequest("POST", "https://ntfy.sh/"+topic, strings.NewReader(message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned %d", resp.StatusCode)
	}
	return nil
}

func startNotificationWorker(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cfg, err := getConfig(db)
			if err != nil || cfg.NtfyTopic == "" {
				continue
			}
			ids, err := getSessionsNeedingNotification(db, cfg.WindDownMinutes)
			if err != nil {
				log.Printf("notification worker: %v", err)
				continue
			}
			for _, id := range ids {
				title := "Time to wind down"
				body := fmt.Sprintf("%s has been awake for %d minutes", cfg.PuppyName, cfg.WindDownMinutes)
				if err := sendNtfyNotification(cfg.NtfyTopic, title, body); err != nil {
					log.Printf("ntfy send: %v", err)
					continue
				}
				if err := markSessionNotified(db, id); err != nil {
					log.Printf("ntfy mark notified: %v", err)
				}
			}
		}
	}()
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

	h := now.Local().Hour()
	isNight := h >= 21 || h < 4

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

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"isPhase": func(p Phase, name string) bool {
			return p == Phase(name)
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
		"sinceMin": func(t *time.Time) int {
			if t == nil {
				return 0
			}
			return int(time.Since(*t).Minutes())
		},
		"fmtMins": func(mins int) string {
			if mins <= 0 {
				return "–"
			}
			h, m := mins/60, mins%60
			if m == 0 {
				return fmt.Sprintf("%dh", h)
			}
			return fmt.Sprintf("%dh %dm", h, m)
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
		"fmtDate": func(date string) string {
			if t, err := time.Parse("2006-01-02", date); err == nil {
				return t.Format("02/01/2006")
			}
			return date
		},
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
					return t.Format("Mon 02/01")
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
	windDownMins, _ := strconv.Atoi(r.FormValue("wind_down_minutes"))

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
	if windDownMins <= 0 {
		windDownMins = current.WindDownMinutes
	}

	cfg := &Config{PuppyName: name, AwakeMinutes: awakeMins, NapMinutes: napMins, WindDownMinutes: windDownMins}
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

	// Wakes before 06:00 local belong to the previous calendar day
	now := time.Now().Local()
	today := now.Format("2006-01-02")
	if now.Hour() < 4 {
		today = now.AddDate(0, 0, -1).Format("2006-01-02")
	}

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

func (a *App) handleUndoPhase(w http.ResponseWriter, r *http.Request) {
	var id int
	var crateAt, sleptAt sql.NullString
	err := a.db.QueryRow(`SELECT id, crate_at, slept_at FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&id, &crateAt, &sleptAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sleptAt.Valid {
		_, err = a.db.Exec(`UPDATE sessions SET slept_at = NULL WHERE id = ?`, id)
	} else if crateAt.Valid {
		_, err = a.db.Exec(`UPDATE sessions SET crate_at = NULL WHERE id = ?`, id)
	} else {
		_, err = a.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	}
	if err != nil {
		log.Printf("handleUndoPhase: %v", err)
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
	if tab != "sleep" && tab != "toilet" {
		tab = "log"
	}

	accidentDays, err := getAccidentFreeDays(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mustJSON := func(v any) template.JS {
		b, _ := json.Marshal(v)
		return template.JS(b)
	}
	sd := &StatsData{Days: days, Config: cfg, Tab: tab, AccidentFreeDays: accidentDays}
	switch tab {
	case "sleep":
		series, err := getSessionSeries(a.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.SettleEasyJSON = mustJSON(series.SettleEasy)
		sd.SettleOkJSON = mustJSON(series.SettleOk)
		sd.SettleHardJSON = mustJSON(series.SettleHard)
		sd.SettleNoneJSON = mustJSON(series.SettleNone)

		// Nap duration by time of day: 12 two-hour buckets, avg + count for JS
		type napBucket struct {
			Avg   float64 `json:"avg"`
			Count int     `json:"count"`
		}
		bucketSum := make([]int, 12)
		bucketCount := make([]int, 12)
		for _, p := range series.NapByTime {
			b := int(p.X / 2)
			if b > 11 {
				b = 11
			}
			bucketSum[b] += p.Y
			bucketCount[b]++
		}
		napBuckets := make([]napBucket, 12)
		for i := range napBuckets {
			napBuckets[i].Count = bucketCount[i]
			if bucketCount[i] > 0 {
				napBuckets[i].Avg = float64(bucketSum[i]) / float64(bucketCount[i])
			}
		}
		sd.NapByTimeJSON = mustJSON(napBuckets)

		// Total sleep per day (ascending order for chart)
		var totalSleep []dailyHours
		for i := len(days) - 1; i >= 0; i-- {
			if days[i].TotalSleepMins > 0 {
				totalSleep = append(totalSleep, dailyHours{
					X: days[i].Date,
					Y: float64(days[i].TotalSleepMins) / 60.0,
				})
			}
		}
		sd.TotalSleepJSON = mustJSON(totalSleep)

		// Settle trend — combine all ease groups for slope calculation

	case "toilet":
		ta, err := getToiletAnalytics(a.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.BucketJSON = mustJSON(ta.Buckets)
		sd.TotalPoops = ta.TotalPoops
		sd.TotalWakes = ta.TotalWakes
		if ta.KDE != nil {
			sd.KDEJSON = mustJSON(ta.KDE)
		}
		if ta.BetaMean != nil {
			sd.BetaJSON = mustJSON(ta.BetaMean)
		}
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
		var sessionDate string
		today := time.Now().Format("2006-01-02")
		if err := a.db.QueryRow(`SELECT date FROM sessions WHERE id = ?`, id).Scan(&sessionDate); err == nil && sessionDate != today {
			http.Redirect(w, r, "/?date="+sessionDate, http.StatusSeeOther)
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

func (a *App) handleToggleToilet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var query string
	switch r.FormValue("value") {
	case "pee":
		query = `UPDATE sessions SET toilet_pee = 1 - toilet_pee WHERE id = ?`
	case "poop":
		query = `UPDATE sessions SET toilet_poop = 1 - toilet_poop WHERE id = ?`
	case "accident":
		query = `UPDATE sessions SET toilet_accident = 1 - toilet_accident WHERE id = ?`
	case "nothing":
		query = `UPDATE sessions SET toilet_pee = 0, toilet_poop = 0, toilet_accident = 0 WHERE id = ?`
	default:
		http.Error(w, "invalid value", http.StatusBadRequest)
		return
	}
	if _, err := a.db.Exec(query, id); err != nil {
		log.Printf("handleToggleToilet: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}

func (a *App) handleNightToilet(w http.ResponseWriter, r *http.Request) {
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
	if err := logNightToilet(a.db, value); err != nil {
		log.Printf("logNightToilet: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderStateFragment(w)
}
