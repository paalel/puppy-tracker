package main

import (
	"database/sql"
	"io/fs"
	"log"
	"net/http"
	"os"

	_ "modernc.org/sqlite"
	_ "time/tzdata" // embed IANA timezone database so TZ env var works on Alpine
)

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
	if err := seedDefaultRoutine(db); err != nil {
		log.Fatalf("seed routine: %v", err)
	}

	tmpl, err := parseTemplates()
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	app := &App{db: db, tmpl: tmpl}

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	mux.HandleFunc("GET /{$}", app.handleIndex)
	mux.HandleFunc("GET /settings", app.handleGetSettings)
	mux.HandleFunc("POST /settings", app.handlePostSettings)
	mux.HandleFunc("GET /stats", app.handleGetStats)
	mux.HandleFunc("POST /api/session/{id}/comment", app.handleSetSessionComment)
	mux.HandleFunc("POST /api/session/{id}/sleep-ease", app.handleSetSessionEnum("sleep_ease", "easy", "ok", "hard"))
	mux.HandleFunc("POST /api/session/{id}/overtired", app.handleToggleSessionBool("overtired"))
	mux.HandleFunc("POST /api/session/{id}/sleep-interrupted", app.handleToggleSessionBool("sleep_interrupted"))
	mux.HandleFunc("POST /api/session/{id}/wake-time", app.handleSetSessionTime("woke_at"))
	mux.HandleFunc("POST /api/session/{id}/crate-time", app.handleSetSessionTime("crate_at"))
	mux.HandleFunc("POST /api/session/{id}/sleep-time", app.handleSetSessionTime("slept_at"))
	mux.HandleFunc("GET /api/state", app.handleGetState)
	mux.HandleFunc("POST /api/phase", app.handlePostPhase)
	mux.HandleFunc("POST /api/meal", app.handlePostMeal)
	mux.HandleFunc("POST /api/wake-adjust", app.handleAdjustSessionTime("woke_at"))
	mux.HandleFunc("POST /api/crate-adjust", app.handleAdjustSessionTime("crate_at"))
	mux.HandleFunc("POST /api/sleep-adjust", app.handleAdjustSessionTime("slept_at"))
	mux.HandleFunc("POST /api/routine/session", app.handleCreateRoutineSession)
	mux.HandleFunc("POST /api/routine/session/{id}", app.handleUpdateRoutineSession)
	mux.HandleFunc("POST /api/routine/session/{id}/delete", app.handleDeleteRoutineSession)
	mux.HandleFunc("POST /api/routine/session/{id}/move/{dir}", app.handleMoveRoutineSession)
	mux.HandleFunc("POST /api/session/{id}/toilet", app.handleSetSessionEnum("toilet", "pee", "poop", "both", "nothing", "accident"))
	mux.HandleFunc("POST /api/night-toilet", app.handleNightToilet)

	log.Println("Puppy Routine Tracker listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
