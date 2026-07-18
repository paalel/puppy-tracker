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
	Days             []DayStat
	Config           *config.Config
	Tab              string
	AwakeJSON        template.JS
	NapJSON          template.JS
	SettleEasyJSON   template.JS
	SettleOkJSON     template.JS
	SettleHardJSON   template.JS
	SettleNoneJSON   template.JS
	AccidentFreeDays    int
	SettleDayEasyJSON   template.JS
	SettleDayOkJSON     template.JS
	SettleDayHardJSON   template.JS
	SettleDayNoneJSON   template.JS
	BucketJSON          template.JS
	KDEJSON          template.JS
	TotalPoops       int
	TotalWakes       int
	TotalSleepJSON   template.JS
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

	accidentDays, err := getAccidentFreeDays(h.db)
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
		series, err := getSessionSeries(h.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.SettleEasyJSON = mustJSON(series.SettleEasy)
		sd.SettleOkJSON = mustJSON(series.SettleOk)
		sd.SettleHardJSON = mustJSON(series.SettleHard)
		sd.SettleNoneJSON = mustJSON(series.SettleNone)

		sd.TotalSleepJSON = mustJSON(totalSleepPoints(days, store.RolloverDate()))

		easy, ok, hard, none, err := getSettleByDay(h.db, store.RolloverDate())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.SettleDayEasyJSON = mustJSON(easy)
		sd.SettleDayOkJSON = mustJSON(ok)
		sd.SettleDayHardJSON = mustJSON(hard)
		sd.SettleDayNoneJSON = mustJSON(none)

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
