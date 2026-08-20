package stats

type dailyHours struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

// accidentTrendPoints converts day stats into chart points for accidents per day,
// oldest first, including today.
func accidentTrendPoints(days []DayStat) []ChartPoint {
	var pts []ChartPoint
	for i := len(days) - 1; i >= 0; i-- {
		pts = append(pts, ChartPoint{X: days[i].Date, Y: days[i].AccidentCount})
	}
	return pts
}

// totalSleepPoints converts day stats into chart points, oldest first, skipping
// days with no sleep data and the current day (which is still in progress).
func totalSleepPoints(days []DayStat, today string) []dailyHours {
	var points []dailyHours
	for i := len(days) - 1; i >= 0; i-- {
		if days[i].Date == today {
			continue
		}
		if days[i].TotalSleepMins > 0 {
			points = append(points, dailyHours{
				X: days[i].Date,
				Y: float64(days[i].TotalSleepMins) / 60.0,
			})
		}
	}
	return points
}
