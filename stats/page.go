package stats

type napBucket struct {
	Avg   float64 `json:"avg"`
	Count int     `json:"count"`
}

type dailyHours struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

// computeNapBuckets groups nap durations into 12 two-hour windows across the day.
func computeNapBuckets(series *SessionSeries) []napBucket {
	bucketSum := make([]int, 12)
	bucketCount := make([]int, 12)
	for _, p := range series.NapByTime {
		b := int(p.X / 2)
		if b > 11 {
			b = 11
		}
		bucketSum[b] += p.Y
		bucketCount[b]++
	}
	buckets := make([]napBucket, 12)
	for i := range buckets {
		buckets[i].Count = bucketCount[i]
		if bucketCount[i] > 0 {
			buckets[i].Avg = float64(bucketSum[i]) / float64(bucketCount[i])
		}
	}
	return buckets
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
