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

type SessionSeries struct {
	Awake      []ChartPoint
	Nap        []ChartPoint
	SettleEasy []ChartPoint
	SettleOk   []ChartPoint
	SettleHard []ChartPoint
	SettleNone []ChartPoint
}

type ToiletAnalytics struct {
	TotalPoops    int
	FirstPoopKDE  []float64 // normalised 0–1, one value per hour 0–23
	SecondPoopKDE []float64
}

type FloatPoint struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

