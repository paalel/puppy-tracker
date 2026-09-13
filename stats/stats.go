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
	AloneMins      int  // total home-alone time that day
	AloneCount     int  // number of home-alone sessions
	AloneConcern   bool // any session stressed or destructive
}

// AloneStats summarises home-alone training: the two records plus quality
// counts and a recent session history.
type AloneStats struct {
	Count           int
	LongestGoodMins int // longest calm, non-destructive session
	LongestAllMins  int // longest by duration, any quality
	CalmCount       int
	UnsettledCount  int
	StressedCount   int
	DestroyedCount  int
	Sessions        []AloneSessionView // recent, newest first
}

type AloneSessionView struct {
	Date         string
	Start        string
	DurationMins int
	Location     string
	Slept        string
	Behaviour    string
	Destroyed    bool
	Note         string
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
