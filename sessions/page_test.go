package sessions

import (
	"testing"
	"time"

	"puppy/config"
	"puppy/routine"
)

func intPtr(i int) *int { return &i }

var testCfg = &config.Config{AwakeMinutes: 40, NapMinutes: 90}

func TestBuildScheduleEmpty(t *testing.T) {
	views := buildSchedule("2026-01-15", nil, nil, testCfg)
	if len(views) != 0 {
		t.Errorf("got %d sessions, want 0", len(views))
	}
}

func TestBuildScheduleClassification(t *testing.T) {
	rs := []routine.RoutineSession{
		{ID: 1, Label: "Morning"},
		{ID: 2, Label: "Mid-morning"},
		{ID: 3, Label: "Lunch"},
	}

	past := time.Now().Add(-2 * time.Hour)
	pastSlept := time.Now().Add(-90 * time.Minute)
	active := time.Now().Add(-20 * time.Minute)

	db := []dbSession{
		{ID: 10, RoutineSessionID: intPtr(1), WokeAt: &past, SleptAt: &pastSlept},
		{ID: 11, RoutineSessionID: intPtr(2), WokeAt: &active},
		// no entry for session 3 → future
	}

	views := buildSchedule("2026-01-15", db, rs, testCfg)
	if len(views) != 3 {
		t.Fatalf("got %d views, want 3", len(views))
	}

	if !views[0].IsPast || views[0].IsActive || views[0].IsFuture {
		t.Errorf("session 0: got IsPast=%v IsActive=%v IsFuture=%v, want IsPast=true",
			views[0].IsPast, views[0].IsActive, views[0].IsFuture)
	}
	if views[1].IsPast || !views[1].IsActive || views[1].IsFuture {
		t.Errorf("session 1: got IsPast=%v IsActive=%v IsFuture=%v, want IsActive=true",
			views[1].IsPast, views[1].IsActive, views[1].IsFuture)
	}
	if views[2].IsPast || views[2].IsActive || !views[2].IsFuture {
		t.Errorf("session 2: got IsPast=%v IsActive=%v IsFuture=%v, want IsFuture=true",
			views[2].IsPast, views[2].IsActive, views[2].IsFuture)
	}
}

func TestBuildPastScheduleNapAndDuration(t *testing.T) {
	woke1 := time.Date(2026, 1, 15, 7, 0, 0, 0, time.UTC)
	slept1 := time.Date(2026, 1, 15, 7, 40, 0, 0, time.UTC)
	woke2 := time.Date(2026, 1, 15, 9, 10, 0, 0, time.UTC) // 90 min after slept1
	db := []dbSession{
		{ID: 10, WokeAt: &woke1, SleptAt: &slept1},
		{ID: 11, WokeAt: &woke2},
	}

	views := buildPastSchedule(db, nil)
	if len(views) != 2 {
		t.Fatalf("got %d views, want 2", len(views))
	}
	// Past durations are greyed, not colour-coded.
	if views[0].DurationClass != "text-stone-400" {
		t.Errorf("past durationClass = %q, want text-stone-400", views[0].DurationClass)
	}
	if views[0].ActualDuration != "40m" {
		t.Errorf("actual duration = %q, want 40m", views[0].ActualDuration)
	}
	// Nap between session 0's sleep and session 1's wake = 90m.
	if views[1].SleepDuration != "1h 30m" {
		t.Errorf("nap = %q, want 1h 30m", views[1].SleepDuration)
	}
}

func TestBuildScheduleAppendsUnmatched(t *testing.T) {
	rs := []routine.RoutineSession{{ID: 1, Label: "Morning"}}
	woke := time.Now().Add(-30 * time.Minute)
	db := []dbSession{
		{ID: 10, RoutineSessionID: intPtr(1), WokeAt: &woke, SleptAt: &woke},
		{ID: 99, WokeAt: &woke}, // no routine slot → appended, active
	}

	views := buildSchedule("2026-01-15", db, rs, testCfg)
	if len(views) != 2 {
		t.Fatalf("got %d views, want 2 (1 routine + 1 appended)", len(views))
	}
	if views[1].ID != 99 || !views[1].IsActive {
		t.Errorf("appended view = ID %d IsActive=%v, want ID 99 active", views[1].ID, views[1].IsActive)
	}
}

// TestBuildScheduleCascade verifies that a subsequent session's planned wake
// follows the previous session's actual sleep time, not the planned time.
func TestBuildScheduleCascade(t *testing.T) {
	rs := []routine.RoutineSession{
		{ID: 1, Label: "Morning"},
		{ID: 2, Label: "Mid-morning"},
		{ID: 3, Label: "Lunch"},
	}

	// First session slept late (11:00 UTC = later than the planned 09:40).
	sleptAt := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)
	db := []dbSession{
		{ID: 10, RoutineSessionID: intPtr(1), SleptAt: &sleptAt},
	}

	views := buildSchedule("2026-01-15", db, rs, testCfg)

	// Session 2 planned wake must be actual sleep + nap (90 min), not planned sleep + nap.
	want := sleptAt.Local().Add(90 * time.Minute)
	if !views[1].PlannedWake.Equal(want) {
		t.Errorf("session 2 planned wake = %v, want %v (actual sleep + nap)", views[1].PlannedWake, want)
	}

	// Session 3 planned wake cascades from session 2's planned sleep.
	wantNext := views[1].PlannedSleep.Add(90 * time.Minute)
	if !views[2].PlannedWake.Equal(wantNext) {
		t.Errorf("session 3 planned wake = %v, want %v (planned sleep + nap)", views[2].PlannedWake, wantNext)
	}
}
