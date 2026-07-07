package stats

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"puppy/config"
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
	AccidentFreeDays int
	BucketJSON       template.JS
	KDEJSON          template.JS
	BetaJSON         template.JS
	TotalPoops       int
	TotalWakes       int
	TotalSleepJSON   template.JS
	NapByTimeJSON    template.JS
}

type dailyHours struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
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
		if ta.BetaMean != nil {
			sd.BetaJSON = mustJSON(ta.BetaMean)
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
