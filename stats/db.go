package stats

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"

	"puppy/store"
)

func parseTimestamp(s string) (time.Time, error) { return store.ParseTimestamp(s) }

func getDayStats(db *sql.DB) ([]DayStat, error) {
	rows, err := db.Query(`
		SELECT
			date,
			COUNT(*) AS cycles,
			CAST(AVG(strftime('%s', slept_at) - strftime('%s', woke_at)) AS INTEGER) AS avg_awake_secs,
			MIN(woke_at) AS first_wake,
			MAX(slept_at) AS last_sleep,
			SUM(CASE WHEN sleep_ease = 'easy'    THEN 1 ELSE 0 END) AS easy_count,
			SUM(CASE WHEN sleep_ease = 'ok'      THEN 1 ELSE 0 END) AS ok_count,
			SUM(CASE WHEN sleep_ease = 'hard'    THEN 1 ELSE 0 END) AS hard_count,
			SUM(CASE WHEN overtired = 1          THEN 1 ELSE 0 END) AS overtired_count,
			SUM(CASE WHEN toilet_accident = 1    THEN 1 ELSE 0 END) AS accident_count
		FROM sessions
		WHERE slept_at IS NOT NULL AND excluded = 0 AND alone = 0
		GROUP BY date
		ORDER BY date DESC
		LIMIT 30
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	today := store.RolloverDate()

	var days []DayStat
	for rows.Next() {
		var d DayStat
		var avgSecs int
		var firstWake, lastSleep string
		if err := rows.Scan(&d.Date, &d.Cycles, &avgSecs, &firstWake, &lastSleep,
			&d.EasyCount, &d.OkCount, &d.HardCount, &d.OvertiredCount,
			&d.AccidentCount); err != nil {
			return nil, err
		}
		d.AvgAwakeMins = avgSecs / 60
		if t, err := parseTimestamp(firstWake); err == nil {
			tl := t.Local()
			d.FirstWake = &tl
		}
		if t, err := parseTimestamp(lastSleep); err == nil {
			tl := t.Local()
			d.LastSleep = &tl
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	napMins, err := queryDateMins(db, `
		SELECT date, CAST(AVG(nap_secs) AS INTEGER) FROM (
			SELECT s1.date,
			       CAST(strftime('%s', s2.woke_at) AS INTEGER) - CAST(strftime('%s', s1.slept_at) AS INTEGER) AS nap_secs
			FROM sessions s1
			INNER JOIN sessions s2
			       ON  s2.date = s1.date
			       AND s2.id   = (SELECT MIN(id) FROM sessions WHERE date = s1.date AND id > s1.id AND woke_at IS NOT NULL)
			WHERE s1.slept_at IS NOT NULL AND s1.excluded = 0 AND s2.excluded = 0
			      AND s1.alone = 0 AND s2.alone = 0 AND nap_secs > 0
		) GROUP BY date
	`)
	if err != nil {
		return nil, err
	}
	for i := range days {
		days[i].AvgNapMins = napMins[days[i].Date]
	}

	settleMins, err := queryDateMins(db, `
		SELECT date, CAST(AVG(settle_secs) AS INTEGER) FROM (
			SELECT date,
			       CAST(strftime('%s', slept_at) AS INTEGER) - CAST(strftime('%s', crate_at) AS INTEGER) AS settle_secs
			FROM sessions
			WHERE crate_at IS NOT NULL AND slept_at IS NOT NULL AND excluded = 0 AND alone = 0 AND settle_secs > 0
		) GROUP BY date
	`)
	if err != nil {
		return nil, err
	}
	for i := range days {
		days[i].AvgSettleMins = settleMins[days[i].Date]
	}

	totalSleepMins, err := queryDateMins(db, `
		SELECT date, SUM(sleep_secs) FROM (
			SELECT s1.date,
			       CAST(strftime('%s', s2.woke_at) AS INTEGER) - CAST(strftime('%s', s1.slept_at) AS INTEGER) AS sleep_secs
			FROM sessions s1
			INNER JOIN sessions s2
			       ON  s2.date = s1.date
			       AND s2.id   = (SELECT MIN(id) FROM sessions WHERE date = s1.date AND id > s1.id AND woke_at IS NOT NULL)
			WHERE s1.slept_at IS NOT NULL

			UNION ALL

			SELECT s1.date,
			       CAST(strftime('%s', s2.woke_at) AS INTEGER) - CAST(strftime('%s', s1.slept_at) AS INTEGER) AS sleep_secs
			FROM sessions s1
			JOIN sessions s2 ON s2.date = date(s1.date, '+1 day')
			WHERE s1.id = (SELECT MAX(id) FROM sessions s3 WHERE s3.date = s1.date AND s3.slept_at IS NOT NULL)
			  AND s2.id = (SELECT MIN(id) FROM sessions s4 WHERE s4.date = s2.date)
			  AND s1.slept_at IS NOT NULL AND s2.woke_at IS NOT NULL
		) WHERE sleep_secs > 0
		  AND date NOT IN (SELECT DISTINCT date FROM sessions WHERE excluded = 1 OR alone = 1)
		GROUP BY date
	`)
	if err != nil {
		return nil, err
	}
	for i := range days {
		days[i].TotalSleepMins = totalSleepMins[days[i].Date]
	}

	// For today, only show LastSleep once all routine sessions are completed.
	var routineCount, completedToday int
	_ = db.QueryRow(`SELECT COUNT(*) FROM routine_sessions`).Scan(&routineCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE date = ? AND slept_at IS NOT NULL`, today).Scan(&completedToday)
	if completedToday < routineCount {
		for i := range days {
			if days[i].Date == today {
				days[i].LastSleep = nil
				break
			}
		}
	}

	return days, nil
}

type AccidentStats struct {
	CurrentStreak int
	RecordStreak  int
}

func getAccidentStats(db *sql.DB) (*AccidentStats, error) {
	// Collect all accident timestamps in order.
	rows, err := db.Query(`
		SELECT woke_at FROM sessions
		WHERE toilet_accident = 1 AND woke_at IS NOT NULL AND COALESCE(alone, 0) = 0
		ORDER BY woke_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accidents []time.Time
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if t, err := parseTimestamp(s); err == nil {
			accidents = append(accidents, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Find start of tracking (first session).
	var firstStr sql.NullString
	err = db.QueryRow(`SELECT MIN(woke_at) FROM sessions WHERE woke_at IS NOT NULL AND COALESCE(excluded,0)=0 AND COALESCE(alone,0)=0`).Scan(&firstStr)
	if err != nil {
		return nil, err
	}
	if !firstStr.Valid {
		return &AccidentStats{}, nil
	}
	start, err := parseTimestamp(firstStr.String)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	// Build list of streak boundaries: [start, acc1, acc2, ..., now]
	// Each gap between adjacent points is a clean streak.
	points := make([]time.Time, 0, len(accidents)+2)
	points = append(points, start)
	points = append(points, accidents...)
	points = append(points, now)

	record := 0
	for i := 1; i < len(points); i++ {
		days := int(points[i].Sub(points[i-1]).Hours() / 24)
		if days > record {
			record = days
		}
	}

	var current int
	if len(accidents) == 0 {
		current = int(now.Sub(start).Hours() / 24)
	} else {
		current = int(now.Sub(accidents[len(accidents)-1]).Hours() / 24)
	}

	return &AccidentStats{CurrentStreak: current, RecordStreak: record}, nil
}

// minTimingSamples gates the median+IQR bars (need a few points for a stable
// quartile); minDensitySamples gates the probability curves, which stay honest
// at lower n because their area shrinks with how rarely the poop happens. With
// ~77 tracked days this shows the 1st/2nd/3rd poop; the 4th (1 sample) is still
// dropped until it recurs.
const (
	minTimingSamples  = 5
	minDensitySamples = 3
)

// getToiletAnalytics groups poops by their ordinal within each day (1st, 2nd, …)
// and summarises each ordinal's timing as median + interquartile range. Ordinals
// with fewer than minTimingSamples observations are dropped rather than shown noisily.
func getToiletAnalytics(db *sql.DB) (*ToiletAnalytics, error) {
	// Select raw UTC timestamps and convert to local time in Go — SQLite's
	// 'localtime' modifier uses the server's zone (UTC on Fly), which would
	// show poop times two hours early. store.ParseTimestamp + .Local() is the
	// same path the rest of the app uses for correct Norwegian time.
	rows, err := db.Query(`
		SELECT date, woke_at FROM sessions
		WHERE toilet_poop = 1 AND woke_at IS NOT NULL
		  AND COALESCE(excluded, 0) = 0 AND COALESCE(alone, 0) = 0
		ORDER BY woke_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group poop times (as local fractional hours) by day, preserving order so
	// each day's slice is already sorted; the index within a day is its ordinal.
	perDay := map[string][]float64{}
	total := 0
	for rows.Next() {
		var date, wokeRaw string
		if err := rows.Scan(&date, &wokeRaw); err != nil {
			return nil, err
		}
		t, err := parseTimestamp(wokeRaw)
		if err != nil {
			continue
		}
		lt := t.Local()
		perDay[date] = append(perDay[date], float64(lt.Hour())+float64(lt.Minute())/60.0)
		total++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byOrdinal := map[int][]float64{}
	for _, times := range perDay {
		for i, hf := range times {
			byOrdinal[i+1] = append(byOrdinal[i+1], hf)
		}
	}

	totalDays, err := countTrackedDays(db)
	if err != nil {
		return nil, err
	}

	ta := &ToiletAnalytics{TotalPoops: total}
	for ordinal := 1; ; ordinal++ {
		times, ok := byOrdinal[ordinal]
		if !ok || len(times) < minDensitySamples {
			break
		}
		// Median + IQR bars need a few more points to be stable than a density.
		if len(times) >= minTimingSamples {
			sorted := append([]float64(nil), times...)
			sort.Float64s(sorted)
			ta.Timings = append(ta.Timings, PoopTiming{
				Label:  ordinalLabel(ordinal),
				Count:  len(sorted),
				Median: percentile(sorted, 0.5),
				P25:    percentile(sorted, 0.25),
				P75:    percentile(sorted, 0.75),
			})
		}
		// Density curve scaled so its area equals P(this poop happens on a day).
		prob := 0.0
		if totalDays > 0 {
			prob = float64(len(times)) / float64(totalDays)
		}
		ta.Densities = append(ta.Densities, PoopDensity{
			Label:       ordinalLabel(ordinal),
			Probability: math.Round(prob*100) / 100,
			Curve:       scaledKDE(times, prob),
		})
	}
	return ta, nil
}

// countTrackedDays counts distinct days with at least one session that counts
// toward stats (not excluded, not alone) — the denominator for poop probability.
func countTrackedDays(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(DISTINCT date) FROM sessions WHERE COALESCE(excluded,0)=0 AND COALESCE(alone,0)=0`,
	).Scan(&n)
	return n, err
}

// scaledKDE returns a 24-point circular Gaussian KDE of the given local hours,
// normalised so the sum of the values (each a 1-hour bin) equals area.
func scaledKDE(times []float64, area float64) []float64 {
	kde := computeCircularKDE(times)
	sum := 0.0
	for _, v := range kde {
		sum += v
	}
	if sum == 0 {
		return kde
	}
	out := make([]float64, len(kde))
	for i, v := range kde {
		out[i] = math.Round(v/sum*area*1000) / 1000
	}
	return out
}

// computeCircularKDE evaluates a Gaussian KDE at the midpoint of each hour,
// wrapping around midnight so 23:30 and 00:30 are treated as close.
func computeCircularKDE(times []float64) []float64 {
	const bandwidth = 1.2
	result := make([]float64, 24)
	for h := 0; h < 24; h++ {
		x := float64(h) + 0.5
		for _, t := range times {
			d := x - t
			if d > 12 {
				d -= 24
			} else if d < -12 {
				d += 24
			}
			result[h] += math.Exp(-0.5 * d * d / (bandwidth * bandwidth))
		}
	}
	return result
}

func ordinalLabel(n int) string {
	switch n {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	default:
		return fmt.Sprintf("%dth", n)
	}
}

// percentile returns the p-quantile (0–1) of a pre-sorted slice via linear
// interpolation between adjacent ranks.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	frac := rank - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// getPeeWeekly returns average pees per tracked (non-excluded) day, one point per week
// since tracking began, labelled by the puppy's age in weeks.
func getPeeWeekly(db *sql.DB, birthdate *time.Time) ([]FloatPoint, error) {
	rows, err := db.Query(`
		SELECT date, SUM(toilet_pee) FROM sessions
		WHERE COALESCE(excluded, 0) = 0 AND COALESCE(alone, 0) = 0
		GROUP BY date ORDER BY date
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type dayPee struct {
		date string
		pees int
	}
	var days []dayPee
	for rows.Next() {
		var date string
		var pees int
		if err := rows.Scan(&date, &pees); err != nil {
			return nil, err
		}
		days = append(days, dayPee{date, pees})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ageWeek := func(t time.Time) int {
		if birthdate != nil {
			return int(t.Sub(*birthdate).Hours() / (24 * 7))
		}
		return 0
	}

	type weekAgg struct{ pees, tracked int }
	weekMap := make(map[int]*weekAgg)
	firstWeek, lastWeek := 9999, -1
	for _, d := range days {
		t, err := time.Parse("2006-01-02", d.date)
		if err != nil {
			continue
		}
		w := ageWeek(t)
		if weekMap[w] == nil {
			weekMap[w] = &weekAgg{}
		}
		weekMap[w].pees += d.pees
		weekMap[w].tracked++
		if w < firstWeek {
			firstWeek = w
		}
		if w > lastWeek {
			lastWeek = w
		}
	}
	if len(weekMap) == 0 {
		return nil, nil
	}

	pts := make([]FloatPoint, 0, lastWeek-firstWeek+1)
	for w := firstWeek; w <= lastWeek; w++ {
		label := fmt.Sprintf("Wk %d", w)
		if agg, ok := weekMap[w]; ok && agg.tracked > 0 {
			avg := math.Round(float64(agg.pees)/float64(agg.tracked)*10) / 10
			pts = append(pts, FloatPoint{X: label, Y: avg})
		} else {
			pts = append(pts, FloatPoint{X: label, Y: 0})
		}
	}
	return pts, nil
}

// minFactorSamples is the fewest with-activity sessions before a settle factor
// is reported — below this the average is too noisy to compare.
const minFactorSamples = 8

// getSettleByActivity measures how each awake-window activity relates to settle
// time. For every activity flag it compares the mean settle of sessions that had
// it against those that didn't, sorted most-helpful (largest speed-up) first.
func getSettleByActivity(db *sql.DB) ([]SettleFactor, error) {
	rows, err := db.Query(`
		SELECT CAST(strftime('%s', slept_at) - strftime('%s', crate_at) AS REAL) / 60.0,
		       physical_activity, mental_activity, environmental_activity, calm_winddown
		FROM sessions
		WHERE crate_at IS NOT NULL AND slept_at IS NOT NULL
		  AND slept_at > crate_at AND COALESCE(excluded, 0) = 0 AND COALESCE(alone, 0) = 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type acc struct {
		withSum, withoutSum float64
		withN, withoutN     int
	}
	labels := []string{"Physical", "Mental", "Environmental", "Calm wind-down"}
	accs := make([]acc, len(labels))

	for rows.Next() {
		var settle float64
		flags := make([]int, len(labels))
		if err := rows.Scan(&settle, &flags[0], &flags[1], &flags[2], &flags[3]); err != nil {
			return nil, err
		}
		for i, on := range flags {
			if on == 1 {
				accs[i].withSum += settle
				accs[i].withN++
			} else {
				accs[i].withoutSum += settle
				accs[i].withoutN++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var factors []SettleFactor
	for i, a := range accs {
		if a.withN < minFactorSamples || a.withoutN == 0 {
			continue
		}
		withAvg := a.withSum / float64(a.withN)
		withoutAvg := a.withoutSum / float64(a.withoutN)
		factors = append(factors, SettleFactor{
			Label:      labels[i],
			WithAvg:    math.Round(withAvg*10) / 10,
			WithoutAvg: math.Round(withoutAvg*10) / 10,
			N:          a.withN,
			DeltaMins:  math.Round((withAvg-withoutAvg)*10) / 10,
		})
	}
	sort.Slice(factors, func(i, j int) bool { return factors[i].DeltaMins < factors[j].DeltaMins })

	// Scale bar widths to the strongest effect, keeping a sliver visible for small ones.
	maxAbs := 0.0
	for _, f := range factors {
		if a := math.Abs(f.DeltaMins); a > maxAbs {
			maxAbs = a
		}
	}
	for i := range factors {
		pct := 8
		if maxAbs > 0 {
			pct = int(math.Round(math.Abs(factors[i].DeltaMins) / maxAbs * 100))
			if pct < 8 {
				pct = 8
			}
		}
		factors[i].BarPct = pct
	}
	return factors, nil
}

// minCountSamples is the fewest sessions before a bucket can be crowned the
// fastest — keeps sparse 3+/4-activity buckets from winning on noise.
const minCountSamples = 20

// getSettleByActivityCount groups sessions by how many activities their awake
// window included (0, 1, 2, 3+) and reports the average settle time of each,
// revealing whether more enrichment keeps helping or starts to backfire.
func getSettleByActivityCount(db *sql.DB) ([]SettleCount, error) {
	rows, err := db.Query(`
		SELECT CAST(strftime('%s', slept_at) - strftime('%s', crate_at) AS REAL) / 60.0,
		       physical_activity + mental_activity + environmental_activity + calm_winddown
		FROM sessions
		WHERE crate_at IS NOT NULL AND slept_at IS NOT NULL
		  AND slept_at > crate_at AND COALESCE(excluded, 0) = 0 AND COALESCE(alone, 0) = 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sums := make([]float64, 4) // buckets 0, 1, 2, 3+
	counts := make([]int, 4)
	for rows.Next() {
		var settle float64
		var k int
		if err := rows.Scan(&settle, &k); err != nil {
			return nil, err
		}
		if k > 3 {
			k = 3
		}
		sums[k] += settle
		counts[k]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	labels := []string{"Nothing", "1 thing", "2 things", "3+ things"}
	var out []SettleCount
	var avgs []float64 // aligned with out, for bar scaling
	maxAvg, bestAvg, bestPos := 0.0, math.MaxFloat64, -1
	for i := range labels {
		if counts[i] == 0 {
			continue
		}
		avg := sums[i] / float64(counts[i])
		if avg > maxAvg {
			maxAvg = avg
		}
		if counts[i] >= minCountSamples && avg < bestAvg {
			bestAvg, bestPos = avg, len(out)
		}
		out = append(out, SettleCount{Label: labels[i], AvgMins: int(math.Round(avg)), N: counts[i]})
		avgs = append(avgs, avg)
	}
	for i := range out {
		if maxAvg > 0 {
			out[i].BarPct = int(math.Round(avgs[i] / maxAvg * 100))
		}
		out[i].Best = i == bestPos
	}
	return out, nil
}

// getSettleWeekly returns average settle time (crate→sleep) per week, labelled by age week.
func getSettleWeekly(db *sql.DB, birthdate *time.Time) ([]FloatPoint, error) {
	rows, err := db.Query(`
		SELECT date,
		       CAST(strftime('%s', slept_at) - strftime('%s', crate_at) AS INTEGER) AS settle_secs
		FROM sessions
		WHERE crate_at IS NOT NULL AND slept_at IS NOT NULL
		  AND slept_at > crate_at AND COALESCE(excluded, 0) = 0 AND COALESCE(alone, 0) = 0
		ORDER BY date
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ageWeek := func(t time.Time) int {
		if birthdate != nil {
			return int(t.Sub(*birthdate).Hours() / (24 * 7))
		}
		return 0
	}

	type weekAgg struct{ totalSecs, n int }
	weekMap := make(map[int]*weekAgg)
	firstWeek, lastWeek := 9999, -1
	for rows.Next() {
		var date string
		var secs int
		if err := rows.Scan(&date, &secs); err != nil {
			return nil, err
		}
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		w := ageWeek(t)
		if weekMap[w] == nil {
			weekMap[w] = &weekAgg{}
		}
		weekMap[w].totalSecs += secs
		weekMap[w].n++
		if w < firstWeek {
			firstWeek = w
		}
		if w > lastWeek {
			lastWeek = w
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(weekMap) == 0 {
		return nil, nil
	}

	pts := make([]FloatPoint, 0, lastWeek-firstWeek+1)
	for w := firstWeek; w <= lastWeek; w++ {
		label := fmt.Sprintf("Wk %d", w)
		if agg, ok := weekMap[w]; ok && agg.n > 0 {
			avg := math.Round(float64(agg.totalSecs)/float64(agg.n)/6) / 10 // secs → mins, 1dp
			pts = append(pts, FloatPoint{X: label, Y: avg})
		} else {
			pts = append(pts, FloatPoint{X: label, Y: 0})
		}
	}
	return pts, nil
}

// getAccidentWeekly returns one ChartPoint per week since tracking began,
// labelled by the puppy's age in weeks ("Wk 8", "Wk 9", …).
// Weeks with no accidents are included as Y=0 so the trend is visible.
func getAccidentWeekly(db *sql.DB, birthdate *time.Time) ([]ChartPoint, error) {
	var firstStr sql.NullString
	if err := db.QueryRow(`SELECT MIN(woke_at) FROM sessions WHERE excluded=0 AND alone=0`).Scan(&firstStr); err != nil || !firstStr.Valid {
		return nil, err
	}
	firstTime, err := parseTimestamp(firstStr.String)
	if err != nil {
		return nil, err
	}

	// ageWeek returns completed weeks of age at time t.
	ageWeek := func(t time.Time) int {
		if birthdate != nil {
			return int(t.Sub(*birthdate).Hours() / (24 * 7))
		}
		return int(t.Sub(firstTime).Hours() / (24 * 7))
	}

	rows, err := db.Query(`SELECT woke_at FROM sessions WHERE toilet_accident = 1 AND COALESCE(alone, 0) = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accByWeek := make(map[int]int)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if t, err := parseTimestamp(s); err == nil {
			accByWeek[ageWeek(t)]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	first := ageWeek(firstTime)
	current := ageWeek(time.Now())
	pts := make([]ChartPoint, 0, current-first+1)
	for w := first; w <= current; w++ {
		pts = append(pts, ChartPoint{X: fmt.Sprintf("Wk %d", w), Y: accByWeek[w]})
	}
	return pts, nil
}

func queryDateMins(db *sql.DB, query string) (map[string]int, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int)
	for rows.Next() {
		var date string
		var secs int
		if err := rows.Scan(&date, &secs); err != nil {
			return nil, err
		}
		m[date] = secs / 60
	}
	return m, rows.Err()
}
