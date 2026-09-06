package alone

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

type Handler struct {
	db   *sql.DB
	tmpl *template.Template
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{db: db, tmpl: tmpl}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/alone", h.handleCard)
	mux.HandleFunc("POST /api/alone/start", h.handleStart)
	mux.HandleFunc("POST /api/alone/end", h.handleEnd)
	mux.HandleFunc("POST /api/alone/marker", h.handleMarker)
}

// ── View models (presentation only) ──────────────────────────────────────────

type CardData struct {
	Active *ActiveView
	Recent []SummaryView
}

type ActiveView struct {
	Elapsed   string
	Resting   bool // inferred current state: asleep?
	AsleepPct int
	Markers   []MarkerView // newest first
}

type MarkerView struct {
	Emoji string
	Label string
	Time  string
}

type SummaryView struct {
	When      string
	Duration  string
	AsleepPct int
	Restless  int
	Toilet    int
}

var markerMeta = map[string]struct{ Emoji, Label string }{
	KindAsleep:   {"😴", "Asleep"},
	KindAwake:    {"👁", "Awake"},
	KindRestless: {"😣", "Restless"},
	KindToilet:   {"🚽", "Toilet"},
}

func (h *Handler) handleCard(w http.ResponseWriter, r *http.Request) {
	h.render(w)
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	if _, err := openSession(h.db); err != nil {
		h.fail(w, "start alone", err)
		return
	}
	h.render(w)
}

func (h *Handler) handleEnd(w http.ResponseWriter, r *http.Request) {
	if err := closeOpenSession(h.db); err != nil {
		h.fail(w, "end alone", err)
		return
	}
	h.render(w)
}

func (h *Handler) handleMarker(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := addMarker(h.db, r.FormValue("kind")); err != nil {
		h.fail(w, "add marker", err)
		return
	}
	h.render(w)
}

func (h *Handler) render(w http.ResponseWriter) {
	data, err := h.buildCard()
	if err != nil {
		h.fail(w, "build alone card", err)
		return
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "alone-card", data); err != nil {
		h.fail(w, "alone-card template", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

func (h *Handler) buildCard() (*CardData, error) {
	now := time.Now()
	data := &CardData{}

	open, err := getOpenSession(h.db)
	if err != nil {
		return nil, err
	}
	if open != nil {
		av := &ActiveView{
			Elapsed:   formatDuration(now.Sub(open.StartedAt)),
			Resting:   restingNow(*open),
			AsleepPct: int(open.asleepFraction(now)*100 + 0.5),
		}
		for i := len(open.Markers) - 1; i >= 0; i-- {
			m := open.Markers[i]
			meta := markerMeta[m.Kind]
			av.Markers = append(av.Markers, MarkerView{
				Emoji: meta.Emoji,
				Label: meta.Label,
				Time:  m.At.Local().Format("15:04"),
			})
		}
		data.Active = av
		return data, nil // hide history while a session is active
	}

	recent, err := getRecentSessions(h.db, 5)
	if err != nil {
		return nil, err
	}
	for _, s := range recent {
		if s.EndedAt == nil {
			continue
		}
		data.Recent = append(data.Recent, SummaryView{
			When:      s.StartedAt.Local().Format("Mon 15:04"),
			Duration:  formatDuration(s.EndedAt.Sub(s.StartedAt)),
			AsleepPct: int(s.asleepFraction(now)*100 + 0.5),
			Restless:  s.count(KindRestless),
			Toilet:    s.count(KindToilet),
		})
	}
	return data, nil
}

// restingNow reports whether the most recent state-changing marker was asleep.
func restingNow(s Session) bool {
	for i := len(s.Markers) - 1; i >= 0; i-- {
		switch s.Markers[i].Kind {
		case KindAsleep:
			return true
		case KindAwake, KindRestless:
			return false
		}
	}
	return false
}

func (h *Handler) fail(w http.ResponseWriter, what string, err error) {
	log.Printf("alone: %s: %v", what, err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func formatDuration(d time.Duration) string {
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
