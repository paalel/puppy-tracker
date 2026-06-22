package main

import (
	"database/sql"
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./puppy.db")
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.handleIndex)
	mux.HandleFunc("GET /settings", app.handleGetSettings)
	mux.HandleFunc("POST /settings", app.handlePostSettings)
	mux.HandleFunc("GET /api/state", app.handleGetState)
	mux.HandleFunc("POST /api/phase", app.handlePostPhase)
	mux.HandleFunc("POST /api/meal", app.handlePostMeal)
	mux.HandleFunc("POST /api/wake-adjust", app.handleAdjustWake)
	mux.HandleFunc("POST /api/sleep-adjust", app.handleAdjustSleep)
	mux.HandleFunc("POST /api/routine/session", app.handleCreateRoutineSession)
	mux.HandleFunc("POST /api/routine/session/{id}", app.handleUpdateRoutineSession)
	mux.HandleFunc("POST /api/routine/session/{id}/delete", app.handleDeleteRoutineSession)
	mux.HandleFunc("POST /api/routine/session/{id}/move/{dir}", app.handleMoveRoutineSession)

	log.Println("Puppy Routine Tracker listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
