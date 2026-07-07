package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	_ "time/tzdata"

	"puppy/config"
	"puppy/notify"
	"puppy/routine"
	"puppy/sessions"
	"puppy/stats"
	"puppy/store"
)

//go:embed migrations
var migrationsFS embed.FS

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

func initDB(db *sql.DB) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		sql, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return err
		}
		if err := runMigration(db, string(sql)); err != nil {
			return err
		}
	}
	return nil
}

func runMigration(db *sql.DB, sql string) error {
	for _, stmt := range strings.Split(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			snippet := stmt
			if len(snippet) > 60 {
				snippet = snippet[:60]
			}
			return fmt.Errorf("migration %q: %w", snippet, err)
		}
	}
	return nil
}

func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
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
			today := store.Today()
			yesterday := store.FormatDate(time.Now().AddDate(0, 0, -1))
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

func main() {
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./puppy.db"
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := initDB(db); err != nil {
		log.Fatalf("init db: %v", err)
	}
	if err := config.EnsureNtfyTopic(db); err != nil {
		log.Fatalf("ensure ntfy topic: %v", err)
	}

	tmpl, err := parseTemplates()
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	notify.Start(db)

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	sessions.New(db, tmpl).RegisterRoutes(mux)
	routine.New(db, tmpl).RegisterRoutes(mux)
	stats.New(db, tmpl).RegisterRoutes(mux)
	config.New(db, tmpl).RegisterRoutes(mux)

	log.Println("Puppy Routine Tracker listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
