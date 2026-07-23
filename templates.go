package main

import (
	"fmt"
	"html/template"
	"math"
	"time"

	"puppy/routine"
	"puppy/sessions"
	"puppy/store"
)

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"div": func(a, b int) int { return a / b },
		"mul": func(a, b int) int { return a * b },
		"isPhase": func(p sessions.Phase, name string) bool {
			return p == sessions.Phase(name)
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
		"currentSession": func(svs []sessions.SessionView) *sessions.SessionView {
			for i := range svs {
				if svs[i].IsActive {
					return &svs[i]
				}
			}
			return nil
		},
		"nextSession": func(svs []sessions.SessionView) *sessions.SessionView {
			for i := range svs {
				if svs[i].IsFuture {
					return &svs[i]
				}
			}
			return nil
		},
		"joinActivities": routine.JoinActivities,
		"fmtDate": func(date string) string {
			if t, err := store.ParseDate(date); err == nil {
				return t.Format("02/01/2006")
			}
			return date
		},
		"dayLabel": func(date string) string {
			today := store.RolloverDate()
			yesterday := store.FormatDate(time.Now().Local().AddDate(0, 0, -1))
			if time.Now().Local().Hour() < store.WakeRolloverHour {
				yesterday = store.FormatDate(time.Now().Local().AddDate(0, 0, -2))
			}
			switch date {
			case today:
				return "Today"
			case yesterday:
				return "Yesterday"
			default:
				if t, err := store.ParseDate(date); err == nil {
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
		"ageWeeks": func(t *time.Time) int {
			if t == nil {
				return 0
			}
			return int(math.Floor(time.Since(*t).Hours() / (24 * 7)))
		},
		"birthdateStr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"poopPct": func(p float64) string {
			return fmt.Sprintf("%d%%", int(p*100))
		},
		"poopCI": func(lo, hi float64) string {
			if lo == 0 && hi == 0 {
				return ""
			}
			return fmt.Sprintf("(%d–%d%%)", int(lo*100), int(hi*100))
		},
		"poopAlert": func(p float64) string {
			switch {
			case p >= 0.5:
				return "poop likely"
			case p >= 0.3:
				return "might poop"
			case p >= 0.1:
				return "probably no poop"
			default:
				return "poop not likely"
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
