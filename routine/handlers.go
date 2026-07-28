package routine

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
)

type Handler struct {
	db   *sql.DB
	tmpl *template.Template
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{db: db, tmpl: tmpl}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/routine/sessions", h.handleGetFrag)
	mux.HandleFunc("POST /api/routine/session", h.handleCreate)
	mux.HandleFunc("POST /api/routine/session/{id}", h.handleUpdate)
	mux.HandleFunc("POST /api/routine/session/{id}/delete", h.handleDelete)
	mux.HandleFunc("POST /api/routine/session/{id}/move/{dir}", h.handleMove)
}

func (h *Handler) handleGetFrag(w http.ResponseWriter, r *http.Request) {
	h.renderFrag(w)
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if err := create(h.db); err != nil {
		log.Printf("createRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFrag(w)
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := update(h.db, id, r.FormValue("label"), r.FormValue("activities")); err != nil {
		log.Printf("updateRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFrag(w)
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := remove(h.db, id); err != nil {
		log.Printf("deleteRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFrag(w)
}

func (h *Handler) handleMove(w http.ResponseWriter, r *http.Request) {
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
	if err := move(h.db, id, dir); err != nil {
		log.Printf("moveRoutineSession: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFrag(w)
}

func (h *Handler) renderFrag(w http.ResponseWriter) {
	sessions, err := GetAll(h.db)
	if err != nil {
		log.Printf("getRoutineSessions: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Query config directly to avoid circular import with the config package.
	firstWake, awakeMins, napMins := "09:00", 60, 120
	rows, _ := h.db.Query(`SELECT key, value FROM config WHERE key IN ('first_wake_time','awake_minutes','nap_minutes')`)
	if rows != nil {
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			switch k {
			case "first_wake_time":
				if v != "" {
					firstWake = v
				}
			case "awake_minutes":
				var n int
				if _, err := fmt.Sscan(v, &n); err == nil && n > 0 {
					awakeMins = n
				}
			case "nap_minutes":
				var n int
				if _, err := fmt.Sscan(v, &n); err == nil && n > 0 {
					napMins = n
				}
			}
		}
		rows.Close()
	}

	section := BuildSection(sessions, firstWake, awakeMins, napMins)
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "routine-sessions", section); err != nil {
		log.Printf("routine-sessions template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}
