package main

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	_ "modernc.org/sqlite"
	_ "time/tzdata"

	"puppy/camera"
	"puppy/config"
	"puppy/routine"
	"puppy/sessions"
	"puppy/stats"
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
	// Statements are split on ';', so strip full-line '--' comments first —
	// otherwise a semicolon inside comment prose would split mid-comment.
	for _, stmt := range strings.Split(stripLineComments(sql), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			// Migrations re-run on every boot, so tolerate errors that just mean
			// "already applied": a re-added column, or a re-dropped column.
			msg := err.Error()
			if strings.Contains(msg, "duplicate column name") ||
				strings.Contains(msg, "no such column") {
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

// stripLineComments removes lines whose first non-space content is '--'.
// Inline trailing comments are left alone (SQLite parses them fine); only
// full-line comments are dropped, before the ';' split.
func stripLineComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
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
	tmpl, err := parseTemplates()
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

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
	camera.New(db, tmpl).RegisterRoutes(mux)

	log.Println("Puppy Routine Tracker listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
