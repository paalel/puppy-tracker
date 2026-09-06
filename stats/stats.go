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

type ToiletAnalytics struct {
	TotalPoops int
	Timings    []PoopTiming
}

type FloatPoint struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

