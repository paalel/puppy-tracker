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

type NumericPoint struct {
	X float64 `json:"x"`
	Y int     `json:"y"`
}

type SessionSeries struct {
	Awake      []ChartPoint
	Nap        []ChartPoint
	SettleEasy []ChartPoint
	SettleOk   []ChartPoint
	SettleHard []ChartPoint
	SettleNone []ChartPoint
	NapByTime  []NumericPoint
}

type ToiletAnalytics struct {
	Buckets    []int
	TotalPoops int
	TotalWakes int
	KDE        []float64
}
