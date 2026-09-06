package stats

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /stats", h.handleGetStats)
}

type StatsData struct {
	Days               []DayStat
	Config             *config.Config
	Tab                string
	TotalSleepJSON     template.JS
	SettleWeeklyJSON   template.JS
	AccidentStats      *AccidentStats
	BucketJSON         template.JS
	KDEJSON            template.JS
	TotalPoops         int
	TotalWakes         int
	AccidentWeeklyJSON template.JS
	TotalAccidents     int
	PeeWeeklyJSON      template.JS
}

func (h *Handler) handleGetStats(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Get(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	days, err := getDayStats(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab != "sleep" && tab != "toilet" {
		tab = "log"
	}

	mustJSON := func(v any) template.JS {
		b, _ := json.Marshal(v)
		return template.JS(b)
	}
	sd := &StatsData{Days: days, Config: cfg, Tab: tab}
	switch tab {
	case "sleep":
		sd.TotalSleepJSON = mustJSON(totalSleepPoints(days, store.RolloverDate()))

		settleWeekly, err := getSettleWeekly(h.db, cfg.Birthdate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.SettleWeeklyJSON = mustJSON(settleWeekly)

	case "toilet":
		ta, err := getToiletAnalytics(h.db)
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
		as, err := getAccidentStats(h.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.AccidentStats = as

		for _, d := range days {
			sd.TotalAccidents += d.AccidentCount
		}
		weekly, err := getAccidentWeekly(h.db, cfg.Birthdate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.AccidentWeeklyJSON = mustJSON(weekly)

		peeWeekly, err := getPeeWeekly(h.db, cfg.Birthdate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.PeeWeeklyJSON = mustJSON(peeWeekly)
	}

	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "stats-page", sd); err != nil {
		log.Printf("stats template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}
