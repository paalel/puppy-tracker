// Package simple serves the pared-down "simplified" version of the app for a
// grown dog: a single Today screen with quick logging for poop, sleep, home-alone
// and outings. It runs alongside the classic version (package sessions); main
// dispatches the home page to whichever the configured mode selects.
//
// Scaffolding note: the card contents are placeholders until the design lands.
// The page wrapper, header, date navigation and mode routing are final.
package simple

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"

	"puppy/config"
	"puppy/store"
)

type Handler struct {
	db   *sql.DB
	tmpl *template.Template
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{db: db, tmpl: tmpl}
}

// RegisterRoutes registers the simplified action/fragment routes. These will be
// filled in as the design is implemented; the Today page itself is served via
// Index, dispatched by main.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {}

type pageData struct {
	Config   *config.Config
	Date     string
	IsToday  bool
	PrevDate string
	NextDate string
}

// Index renders the simplified Today screen for the requested date (defaulting
// to today, and never past today), with day-at-a-time navigation.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Get(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	today := store.RolloverDate()
	date := r.URL.Query().Get("date")
	if date == "" || date > today {
		date = today
	}
	d, _ := store.ParseDate(date)
	prev := store.FormatDate(d.AddDate(0, 0, -1))
	var next string
	if date != today {
		if n := store.FormatDate(d.AddDate(0, 0, 1)); n <= today {
			next = n
		}
	}

	data := &pageData{Config: cfg, Date: date, IsToday: date == today, PrevDate: prev, NextDate: next}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "simple-page", data); err != nil {
		log.Printf("simple-page template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}
