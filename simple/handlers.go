// Package simple serves the pared-down "simplified" version of the app for a
// grown dog: a single Today screen with quick logging for poop, sleep, home-alone
// and outings. It runs alongside the classic version (package sessions); main
// dispatches the home page to whichever the configured mode selects.
package simple

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"

	"puppy/store"
)

type Handler struct {
	db   *sql.DB
	tmpl *template.Template
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{db: db, tmpl: tmpl}
}

// RegisterRoutes registers the simplified action/fragment routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /simple/poops", h.handlePoopAdd)
	mux.HandleFunc("DELETE /simple/poops/{id}", h.handlePoopDelete)

	mux.HandleFunc("POST /simple/naps/start", h.handleNapStart)
	mux.HandleFunc("POST /simple/naps/stop", h.handleNapStop)
	mux.HandleFunc("POST /simple/naps/discard", h.handleNapDiscard)
	mux.HandleFunc("DELETE /simple/naps/{id}", h.handleNapDelete)

	mux.HandleFunc("POST /simple/alone/start", h.handleAloneStart)
	mux.HandleFunc("POST /simple/alone/stop", h.handleAloneStop)
	mux.HandleFunc("PATCH /simple/alone/{id}", h.handleAlonePatch)
	mux.HandleFunc("DELETE /simple/alone/{id}", h.handleAloneDelete)
	mux.HandleFunc("GET /simple/alone/card", h.handleAloneCard)

	mux.HandleFunc("GET /simple/outings/new", h.handleOutingNew)
	mux.HandleFunc("GET /simple/outings/{id}/edit", h.handleOutingEdit)
	mux.HandleFunc("POST /simple/outings", h.handleOutingCreate)
	mux.HandleFunc("PUT /simple/outings/{id}", h.handleOutingUpdate)
	mux.HandleFunc("DELETE /simple/outings/{id}", h.handleOutingDelete)
	mux.HandleFunc("GET /simple/outings/card", h.handleOutingsCard)

	mux.HandleFunc("POST /simple/undo/{token}", h.handleUndo)
}

// Index renders the simplified Today screen for the requested date (defaulting
// to today, and never past today), with day-at-a-time navigation.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	today := store.RolloverDate()
	date := r.URL.Query().Get("date")
	if date == "" || date > today {
		date = today
	}

	vm, err := h.buildPage(date)
	if err != nil {
		log.Printf("simple buildPage: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d, _ := store.ParseDate(date)
	vm.IsToday = date == today
	vm.PrevDate = store.FormatDate(d.AddDate(0, 0, -1))
	if date != today {
		if n := store.FormatDate(d.AddDate(0, 0, 1)); n <= today {
			vm.NextDate = n
		}
	}

	h.render(w, "simple-page", vm)
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("simple template %s: %v", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}
