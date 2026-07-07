package config

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

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
	mux.HandleFunc("GET /settings", h.handleGetSettings)
	mux.HandleFunc("POST /settings", h.handlePostSettings)
}

type SettingsData struct {
	Config          *Config
	RoutineSessions []routine.RoutineSession
}

func (h *Handler) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := Get(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rs, err := routine.GetAll(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "settings-page", &SettingsData{Config: cfg, RoutineSessions: rs}); err != nil {
		log.Printf("settings template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (h *Handler) handlePostSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("puppy_name"))
	awakeMins, _ := strconv.Atoi(r.FormValue("awake_minutes"))
	napMins, _ := strconv.Atoi(r.FormValue("nap_minutes"))
	windDownMins, _ := strconv.Atoi(r.FormValue("wind_down_minutes"))

	current, err := Get(h.db)
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
	if err := Save(h.db, cfg); err != nil {
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
