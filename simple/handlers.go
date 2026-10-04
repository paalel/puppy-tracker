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

// RegisterRoutes registers the simplified action/fragment routes (added next stage).
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {}

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
