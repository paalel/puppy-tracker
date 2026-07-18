package stats

import "testing"

func TestTotalSleepPointsOrderAndFilter(t *testing.T) {
	days := []DayStat{
		{Date: "2026-01-15", TotalSleepMins: 600},
		{Date: "2026-01-14", TotalSleepMins: 0}, // should be skipped
		{Date: "2026-01-13", TotalSleepMins: 540},
	}
	pts := totalSleepPoints(days, "2026-01-16") // today not in set, so no filtering

	if len(pts) != 2 {
		t.Fatalf("got %d points, want 2 (zero-sleep day skipped)", len(pts))
	}
	// getDayStats returns newest-first; totalSleepPoints reverses to oldest-first.
	if pts[0].X != "2026-01-13" {
		t.Errorf("pts[0].X = %q, want oldest date first", pts[0].X)
	}
	if pts[1].X != "2026-01-15" {
		t.Errorf("pts[1].X = %q, want newest date last", pts[1].X)
	}
	if pts[0].Y != 9.0 {
		t.Errorf("pts[0].Y = %v, want 9.0 (540 mins / 60)", pts[0].Y)
	}
}
