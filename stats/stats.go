package stats

import "time"

type DayStat struct {
	Date           string
	Cycles         int
	AvgAwakeMins   int
	AvgNapMins     int
	AvgSettleMins  int
	TotalSleepMins int
	FirstWake      *time.Time
	LastSleep      *time.Time
	EasyCount      int
	OkCount        int
	HardCount      int
	OvertiredCount int
	AccidentCount  int
}

type ChartPoint struct {
	X string `json:"x"`
	Y int    `json:"y"`
}

// PoopTiming summarises when the Nth poop of the day typically happens,
// as fractional local hours (e.g. 8.5 = 08:30). Median with p25–p75 range.
type PoopTiming struct {
	Label  string  `json:"label"`
	Count  int     `json:"count"`
	Median float64 `json:"median"`
	P25    float64 `json:"p25"`
	P75    float64 `json:"p75"`
}

// PoopDensity is a time-of-day probability curve for the Nth poop of the day.
// Curve holds one density value per hour (0–23); the area under it (sum of the
// values, each spanning a 1-hour bin) equals Probability — the chance that an
// Nth poop happens on a given day.
type PoopDensity struct {
	Label       string    `json:"label"`
	Probability float64   `json:"probability"` // 0–1
	Curve       []float64 `json:"curve"`
}

type ToiletAnalytics struct {
	TotalPoops int
	Timings    []PoopTiming
	Densities  []PoopDensity
}

type FloatPoint struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

// SettleFactor compares average settle time (crate→sleep) for sessions whose
// preceding awake window included a given activity vs those that didn't.
// DeltaMins < 0 means that activity is associated with settling faster.
type SettleFactor struct {
	Label      string
	WithAvg    float64
	WithoutAvg float64
	N          int     // sessions that had the activity
	DeltaMins  float64 // WithAvg − WithoutAvg
	BarPct     int     // bar width 0–100, scaled to the strongest effect
}

// SettleCount summarises settle time by how many activities the awake window
// included (0, 1, 2, 3+) — showing whether doing more helps or backfires.
type SettleCount struct {
	Label   string
	AvgMins int
	N       int
	BarPct  int  // scaled to the slowest bucket
	Best    bool // fastest bucket among those with enough data
}
