package stats

import "testing"

func TestComputeNapBucketsAlwaysReturns12(t *testing.T) {
	for _, series := range []*SessionSeries{
		{},
		{NapByTime: []NumericPoint{{X: 9.5, Y: 60}}},
	} {
		if got := len(computeNapBuckets(series)); got != 12 {
			t.Errorf("len = %d, want 12", got)
		}
	}
}

func TestComputeNapBucketAssignment(t *testing.T) {
	series := &SessionSeries{
		NapByTime: []NumericPoint{
			{X: 0.5, Y: 90},  // 00:30 → bucket 0 (0–2h)
			{X: 9.0, Y: 60},  // 09:00 → bucket 4 (8–10h)
			{X: 9.5, Y: 80},  // 09:30 → bucket 4
			{X: 23.9, Y: 50}, // 23:54 → bucket 11 (clamped)
		},
	}
	buckets := computeNapBuckets(series)

	if buckets[0].Count != 1 || buckets[0].Avg != 90 {
		t.Errorf("bucket[0] = {%d, %.0f}, want {1, 90}", buckets[0].Count, buckets[0].Avg)
	}
	if buckets[4].Count != 2 || buckets[4].Avg != 70 {
		t.Errorf("bucket[4] = {%d, %.0f}, want {2, 70}", buckets[4].Count, buckets[4].Avg)
	}
	if buckets[11].Count != 1 {
		t.Errorf("bucket[11].Count = %d, want 1 (clamp at 11)", buckets[11].Count)
	}
}

func TestTotalSleepPointsOrderAndFilter(t *testing.T) {
	days := []DayStat{
		{Date: "2026-01-15", TotalSleepMins: 600},
		{Date: "2026-01-14", TotalSleepMins: 0}, // should be skipped
		{Date: "2026-01-13", TotalSleepMins: 540},
	}
	pts := totalSleepPoints(days)

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
