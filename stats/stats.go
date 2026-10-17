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
	// Classic metrics (above) only apply when the day was tracked with the
	// classic nap flow. Simplified days carry the fields below instead.
	HasClassic      bool
	SimplePoops     int
	SimpleAccidents int
	SimpleNaps      int
	SimpleNapMins   int
}

// HasSimple reports whether the day has any simplified data worth summarising.
func (d DayStat) HasSimple() bool {
	return d.SimplePoops > 0 || d.SimpleAccidents > 0 || d.SimpleNaps > 0 || d.AloneCount > 0
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

// AloneWeek is one week's home-alone time split by how it went, for a stacked
// bar chart: volume (total height) and quality (segment mix) at a glance.
type AloneWeek struct {
	Label         string `json:"label"`
	CalmMins      int    `json:"calm"`
	UnsettledMins int    `json:"unsettled"`
	StressedMins  int    `json:"stressed"`
	OtherMins     int    `json:"other"` // unrated
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

// ── Training ────────────────────────────────────────────────────────────────

// TrainingStats is the Training tab payload: one GitHub-style contribution grid
// per skill. The cell colour shows how well the day's target was met (adherence)
// and where the coloured cells sit over the shared week columns shows the
// routine — when each skill was trained. All skills share the same week columns
// so the grids line up and skills that stopped read as trailing-off activity.
type TrainingStats struct {
	Skills     []SkillGrid
	FirstLabel string // Monday of the earliest week column (grid left edge)
}

type SkillGrid struct {
	Name      string
	Weekly    bool
	Target    int
	Active    bool // still in the routine (not removed)
	TotalReps int
	MetRate   int         // % of active days (or weeks) the target was met
	Weeks     [][]DayCell // week columns, each 7 cells Mon→Sun
}

// DayCell is one day in a skill's grid. Level sets the colour: -1 out-of-range
// (future / before the window), 0 none, 1–4 increasing share of target met.
type DayCell struct {
	Date  string
	Reps  int
	Level int
}
