package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"puppy/config"
	"puppy/routine"
	"puppy/sessions"
	"puppy/stats"
)

func newTestApp(t *testing.T) http.Handler {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)

	if err := initDB(db); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parseTemplates: %v", err)
	}

	mux := http.NewServeMux()
	sessions.New(db, tmpl).RegisterRoutes(mux)
	routine.New(db, tmpl).RegisterRoutes(mux)
	stats.New(db, tmpl).RegisterRoutes(mux)
	config.New(db, tmpl).RegisterRoutes(mux)
	return mux
}

func get(t *testing.T, app http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func post(t *testing.T, app http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.ServeHTTP(w, r)
	return w
}

// Smoke tests — catch template errors and nil panics across all pages.

func TestGetIndex(t *testing.T) {
	app := newTestApp(t)
	if w := get(t, app, "/"); w.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", w.Code)
	}
}

func TestGetState(t *testing.T) {
	app := newTestApp(t)
	if w := get(t, app, "/api/state"); w.Code != http.StatusOK {
		t.Errorf("GET /api/state = %d, want 200", w.Code)
	}
}

func TestGetStats(t *testing.T) {
	app := newTestApp(t)
	for _, tab := range []string{"", "sleep", "toilet"} {
		path := "/stats"
		if tab != "" {
			path += "?tab=" + tab
		}
		if w := get(t, app, path); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}

func TestGetSettings(t *testing.T) {
	app := newTestApp(t)
	if w := get(t, app, "/settings"); w.Code != http.StatusOK {
		t.Errorf("GET /settings = %d, want 200", w.Code)
	}
}

// Phase transitions — the core loop.

func TestPhaseCycle(t *testing.T) {
	app := newTestApp(t)

	steps := []sessions.Phase{sessions.PhaseActive, sessions.PhaseCrating, sessions.PhaseSleeping}
	for _, phase := range steps {
		w := post(t, app, "/api/phase", url.Values{"phase": {string(phase)}})
		if w.Code != http.StatusOK {
			t.Errorf("POST /api/phase phase=%s = %d, want 200", phase, w.Code)
		}
	}

	// Index must still render cleanly after a completed cycle.
	if w := get(t, app, "/"); w.Code != http.StatusOK {
		t.Errorf("GET / after cycle = %d, want 200", w.Code)
	}
}

func TestPhaseUndo(t *testing.T) {
	app := newTestApp(t)
	post(t, app, "/api/phase", url.Values{"phase": {string(sessions.PhaseActive)}})
	post(t, app, "/api/phase", url.Values{"phase": {string(sessions.PhaseCrating)}})

	w := post(t, app, "/api/phase/undo", url.Values{})
	if w.Code != http.StatusOK {
		t.Errorf("POST /api/phase/undo = %d, want 200", w.Code)
	}
}

func TestPhaseInvalidRejected(t *testing.T) {
	app := newTestApp(t)
	w := post(t, app, "/api/phase", url.Values{"phase": {"invalid"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/phase invalid = %d, want 400", w.Code)
	}
}

// Config — save and re-render.

func TestSaveConfig(t *testing.T) {
	app := newTestApp(t)
	w := post(t, app, "/settings", url.Values{
		"puppy_name":        {"Buddy"},
		"awake_minutes":     {"45"},
		"nap_minutes":       {"80"},
		"wind_down_minutes": {"20"},
	})
	if w.Code != http.StatusSeeOther {
		t.Errorf("POST /settings = %d, want 303", w.Code)
	}
	if w := get(t, app, "/settings"); w.Code != http.StatusOK {
		t.Errorf("GET /settings after save = %d, want 200", w.Code)
	}
}
