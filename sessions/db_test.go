package sessions

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestGetSleptAtBefore(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (id INTEGER PRIMARY KEY, slept_at DATETIME)`); err != nil {
		t.Fatal(err)
	}
	// Two sleeps: an overnight one and an earlier nap.
	db.Exec(`INSERT INTO sessions (slept_at) VALUES (?)`, "2026-01-15 12:00:00")
	db.Exec(`INSERT INTO sessions (slept_at) VALUES (?)`, "2026-01-15 20:30:00") // bedtime

	// Waking next morning: previous sleep is bedtime the night before.
	wake := time.Date(2026, 1, 16, 7, 0, 0, 0, time.UTC)
	got, err := getSleptAtBefore(db, wake)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected a previous sleep, got nil")
	}
	if want := time.Date(2026, 1, 15, 20, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v (most recent sleep before wake)", got.UTC(), want)
	}

	// Nothing before the earliest sleep.
	early := time.Date(2026, 1, 15, 6, 0, 0, 0, time.UTC)
	if got, err := getSleptAtBefore(db, early); err != nil || got != nil {
		t.Errorf("expected nil before any sleep, got %v (err %v)", got, err)
	}
}
