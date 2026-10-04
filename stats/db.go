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
	byDate := map[string]*DayStat{}
	get := func(date string) *DayStat {
		if byDate[date] == nil {
			byDate[date] = &DayStat{Date: date}
		}
		return byDate[date]
	}

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
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	today := store.RolloverDate()

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
		d.HasClassic = true
		if t, err := parseTimestamp(firstWake); err == nil {
			tl := t.Local()
			d.FirstWake = &tl
		}
		if t, err := parseTimestamp(lastSleep); err == nil {
			tl := t.Local()
			d.LastSleep = &tl
		}
		byDate[d.Date] = &d
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
	for date, v := range napMins {
		if d := byDate[date]; d != nil {
			d.AvgNapMins = v
		}
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
	for date, v := range settleMins {
		if d := byDate[date]; d != nil {
			d.AvgSettleMins = v
		}
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
	for date, v := range totalSleepMins {
		if d := byDate[date]; d != nil {
			d.TotalSleepMins = v
		}
	}

	// Merge simplified per-day summaries, creating entries for purely-simplified days.
	simpleByDay, err := getSimpleByDay(db)
	if err != nil {
		return nil, err
	}
	for date, s := range simpleByDay {
		d := get(date)
		d.SimplePoops = s.poops
		d.SimpleAccidents = s.accidents
		d.SimpleNaps = s.naps
		d.SimpleNapMins = s.napMins
		d.SimpleOutings = s.outings
	}

	// Merge home-alone time per day (shared sessions alone rows).
	aloneByDay, err := getAloneByDay(db)
	if err != nil {
		return nil, err
	}
	for date, a := range aloneByDay {
		d := get(date)
		d.AloneMins = a.AloneMins
		d.AloneCount = a.AloneCount
		d.AloneConcern = a.AloneConcern
	}

	// For today, only show LastSleep once all routine sessions are completed.
	var routineCount, completedToday int
	_ = db.QueryRow(`SELECT COUNT(*) FROM routine_sessions`).Scan(&routineCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE date = ? AND slept_at IS NOT NULL`, today).Scan(&completedToday)
	if completedToday < routineCount {
		if d := byDate[today]; d != nil {
			d.LastSleep = nil
		}
	}

	// Newest first, capped at 30 days.
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) > 30 {
		dates = dates[:30]
	}
	days := make([]DayStat, 0, len(dates))
	for _, date := range dates {
		days = append(days, *byDate[date])
	}
	return days, nil
}

type simpleDaySummary struct {
	poops, accidents, naps, napMins, outings int
}

// getSimpleByDay aggregates simplified poops/naps/outings per day for the History.
func getSimpleByDay(db *sql.DB) (map[string]*simpleDaySummary, error) {
	m := map[string]*simpleDaySummary{}
	get := func(day string) *simpleDaySummary {
		if m[day] == nil {
			m[day] = &simpleDaySummary{}
		}
		return m[day]
	}

	pr, err := db.Query(`SELECT day,
		SUM(CASE WHEN kind='poop' THEN 1 ELSE 0 END),
		SUM(CASE WHEN kind='accident' THEN 1 ELSE 0 END)
		FROM poops WHERE deleted_at IS NULL GROUP BY day`)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var day string
		var poops, accidents int
		if err := pr.Scan(&day, &poops, &accidents); err != nil {
			pr.Close()
			return nil, err
		}
		s := get(day)
		s.poops, s.accidents = poops, accidents
	}
	pr.Close()

	nr, err := db.Query(`SELECT day, COUNT(*),
		CAST(SUM(strftime('%s', ended_at) - strftime('%s', started_at)) / 60 AS INTEGER)
		FROM naps WHERE ended_at IS NOT NULL AND deleted_at IS NULL GROUP BY day`)
	if err != nil {
		return nil, err
	}
	for nr.Next() {
		var day string
		var naps, mins int
		if err := nr.Scan(&day, &naps, &mins); err != nil {
			nr.Close()
			return nil, err
		}
		s := get(day)
		s.naps, s.napMins = naps, mins
	}
	nr.Close()

	or, err := db.Query(`SELECT day, COUNT(*) FROM outings WHERE deleted_at IS NULL GROUP BY day`)
	if err != nil {
		return nil, err
	}
	for or.Next() {
		var day string
		var n int
		if err := or.Scan(&day, &n); err != nil {
			or.Close()
			return nil, err
		}
		get(day).outings = n
	}
	or.Close()
	return m, nil
}

type AccidentStats struct {
	CurrentStreak int
	RecordStreak  int
}

// getAloneStats summarises completed home-alone sessions: the two records
// (longest good = calm and non-destructive; longest overall), quality counts,
// and a recent session history (newest first, capped).
func getAloneStats(db *sql.DB) (*AloneStats, error) {
	rows, err := db.Query(`
		SELECT date, woke_at, slept_at,
		       COALESCE(alone_slept, ''), COALESCE(alone_behaviour, ''),
		       COALESCE(alone_location, ''), COALESCE(alone_destroyed, 0), COALESCE(comment, '')
		FROM sessions
		WHERE alone = 1 AND woke_at IS NOT NULL AND slept_at IS NOT NULL
		ORDER BY woke_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	const historyLimit = 12
	st := &AloneStats{}
	for rows.Next() {
		var date, wokeRaw, sleptRaw, slept, behaviour, location, note string
		var destroyedInt int
		if err := rows.Scan(&date, &wokeRaw, &sleptRaw, &slept, &behaviour, &location, &destroyedInt, &note); err != nil {
			return nil, err
		}
		woke, err := parseTimestamp(wokeRaw)
		if err != nil {
			continue
		}
		end, err := parseTimestamp(sleptRaw)
		if err != nil {
			continue
		}
		mins := int(end.Sub(woke).Minutes())
		if mins < 0 {
			continue
		}
		destroyed := destroyedInt == 1

		st.Count++
		if mins > st.LongestAllMins {
			st.LongestAllMins = mins
		}
		if behaviour == "calm" && !destroyed && mins > st.LongestGoodMins {
			st.LongestGoodMins = mins
		}
		switch behaviour {
		case "calm":
			st.CalmCount++
		case "unsettled":
			st.UnsettledCount++
		case "stressed":
			st.StressedCount++
		}
		if destroyed {
			st.DestroyedCount++
		}
		if len(st.Sessions) < historyLimit {
			st.Sessions = append(st.Sessions, AloneSessionView{
				Date:         date,
				Start:        woke.Local().Format("15:04"),
				DurationMins: mins,
				Location:     location,
				Slept:        slept,
				Behaviour:    behaviour,
				Destroyed:    destroyed,
				Note:         note,
			})
		}
	}
	return st, rows.Err()
}

// getAloneWeekly returns home-alone minutes per age-week, split by behaviour,
// from the first week with any alone session through the current week (empty
// weeks in between are kept as zero bars so a training gap is visible).
func getAloneWeekly(db *sql.DB, birthdate *time.Time) ([]AloneWeek, error) {
	rows, err := db.Query(`
		SELECT date,
		       CAST((strftime('%s', slept_at) - strftime('%s', woke_at)) / 60 AS INTEGER),
		       COALESCE(alone_behaviour, '')
		FROM sessions
		WHERE alone = 1 AND woke_at IS NOT NULL AND slept_at IS NOT NULL
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

	weekMap := make(map[int]*AloneWeek)
	firstWeek, lastWeek := 9999, -1
	for rows.Next() {
		var date, behaviour string
		var mins int
		if err := rows.Scan(&date, &mins, &behaviour); err != nil {
			return nil, err
		}
		if mins <= 0 {
			continue
		}
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		w := ageWeek(t)
		if weekMap[w] == nil {
			weekMap[w] = &AloneWeek{}
		}
		switch behaviour {
		case "calm":
			weekMap[w].CalmMins += mins
		case "unsettled":
			weekMap[w].UnsettledMins += mins
		case "stressed":
			weekMap[w].StressedMins += mins
		default:
			weekMap[w].OtherMins += mins
		}
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
	if lastWeek < 0 {
		return nil, nil
	}

	out := make([]AloneWeek, 0, lastWeek-firstWeek+1)
	for w := firstWeek; w <= lastWeek; w++ {
		wk := AloneWeek{Label: fmt.Sprintf("Wk %d", w)}
		if agg := weekMap[w]; agg != nil {
			wk.CalmMins, wk.UnsettledMins = agg.CalmMins, agg.UnsettledMins
			wk.StressedMins, wk.OtherMins = agg.StressedMins, agg.OtherMins
		}
		out = append(out, wk)
	}
	return out, nil
}

// getAloneByDay aggregates home-alone time per day for the History cards.
func getAloneByDay(db *sql.DB) (map[string]*DayStat, error) {
	rows, err := db.Query(`
		SELECT date,
		       CAST((strftime('%s', slept_at) - strftime('%s', woke_at)) / 60 AS INTEGER),
		       COALESCE(alone_behaviour, ''), COALESCE(alone_destroyed, 0)
		FROM sessions
		WHERE alone = 1 AND woke_at IS NOT NULL AND slept_at IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := map[string]*DayStat{}
	for rows.Next() {
		var date, behaviour string
		var mins, destroyed int
		if err := rows.Scan(&date, &mins, &behaviour, &destroyed); err != nil {
			return nil, err
		}
		d := byDate[date]
		if d == nil {
			d = &DayStat{}
			byDate[date] = d
		}
		if mins > 0 {
			d.AloneMins += mins
		}
		d.AloneCount++
		if behaviour == "stressed" || destroyed == 1 {
			d.AloneConcern = true
		}
	}
	return byDate, rows.Err()
}

// accidentTimes returns all accident timestamps from BOTH classic sessions and
// the simplified poops table (kind='accident'), parsed as UTC and sorted.
func accidentTimes(db *sql.DB) ([]time.Time, error) {
	var out []time.Time
	collect := func(query string) error {
		rows, err := db.Query(query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			if t, err := parseTimestamp(s); err == nil {
				out = append(out, t)
			}
		}
		return rows.Err()
	}
	if err := collect(`SELECT woke_at FROM sessions WHERE toilet_accident = 1 AND woke_at IS NOT NULL AND COALESCE(alone,0) = 0`); err != nil {
		return nil, err
	}
	if err := collect(`SELECT at FROM poops WHERE kind = 'accident' AND deleted_at IS NULL`); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

func getAccidentStats(db *sql.DB) (*AccidentStats, error) {
	accidents, err := accidentTimes(db)
	if err != nil {
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

// recencyHalfLifeDays matches the live predictor: a poop observation this many
// days older than the most recent tracked day carries half the influence, so the
// timing and probability reflect her current pattern as her feeding schedule
// (and thus her poop schedule) shifts with age. All data is still used.
const recencyHalfLifeDays = 28

// wobs is a recency-weighted observation: a local fractional hour with a weight.
type wobs struct{ h, w float64 }

// poopEventsByDay gathers poop timestamps from classic sessions and the simplified
// poops table, grouped by day and sorted by time within each day. Returns the
// per-day times and the total count.
func poopEventsByDay(db *sql.DB) (map[string][]time.Time, int, error) {
	perDay := map[string][]time.Time{}
	add := func(day, raw string) {
		if t, err := parseTimestamp(raw); err == nil {
			perDay[day] = append(perDay[day], t.Local())
		}
	}

	rows, err := db.Query(`SELECT date, woke_at FROM sessions
		WHERE toilet_poop = 1 AND woke_at IS NOT NULL
		  AND COALESCE(excluded,0)=0 AND COALESCE(alone,0)=0`)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var d, w string
		if err := rows.Scan(&d, &w); err != nil {
			rows.Close()
			return nil, 0, err
		}
		add(d, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	pr, err := db.Query(`SELECT day, at FROM poops WHERE kind = 'poop' AND deleted_at IS NULL`)
	if err != nil {
		return nil, 0, err
	}
	for pr.Next() {
		var d, a string
		if err := pr.Scan(&d, &a); err != nil {
			pr.Close()
			return nil, 0, err
		}
		add(d, a)
	}
	pr.Close()
	if err := pr.Err(); err != nil {
		return nil, 0, err
	}

	total := 0
	for day := range perDay {
		ts := perDay[day]
		sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
		total += len(ts)
	}
	return perDay, total, nil
}

func getToiletAnalytics(db *sql.DB) (*ToiletAnalytics, error) {
	// Poop events come from BOTH versions: classic sessions (toilet_poop) and the
	// simplified poops table. Timestamps are converted to local in Go (SQLite's
	// 'localtime' is UTC on the server), then grouped and sorted per day so the
	// ordinal (1st/2nd/…) is correct across the two sources.
	perDay, total, err := poopEventsByDay(db)
	if err != nil {
		return nil, err
	}

	// Recency weight per day: decays from the most recent tracked day. The same
	// weight is the probability denominator's per-day contribution, so P(Nth poop)
	// = weighted days with an Nth poop / weighted total tracked days.
	dayWeight, weightedTotalDays, err := trackedDayWeights(db)
	if err != nil {
		return nil, err
	}

	byOrdinal := map[int][]wobs{}
	for date, times := range perDay {
		w := dayWeight[date]
		for i, t := range times {
			hf := float64(t.Hour()) + float64(t.Minute())/60.0
			byOrdinal[i+1] = append(byOrdinal[i+1], wobs{h: hf, w: w})
		}
	}

	ta := &ToiletAnalytics{TotalPoops: total}
	for ordinal := 1; ; ordinal++ {
		obs, ok := byOrdinal[ordinal]
		if !ok || len(obs) < minDensitySamples { // gate on raw count so rare poops still show
			break
		}
		// Median + IQR bars need a few more points to be stable than a density.
		if len(obs) >= minTimingSamples {
			sorted := append([]wobs(nil), obs...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].h < sorted[j].h })
			ta.Timings = append(ta.Timings, PoopTiming{
				Label:  ordinalLabel(ordinal),
				Count:  len(sorted),
				Median: weightedPercentile(sorted, 0.5),
				P25:    weightedPercentile(sorted, 0.25),
				P75:    weightedPercentile(sorted, 0.75),
			})
		}
		// Density curve scaled so its area equals P(this poop happens on a day).
		var wnum float64
		for _, o := range obs {
			wnum += o.w
		}
		prob := 0.0
		if weightedTotalDays > 0 {
			prob = math.Min(1, wnum/weightedTotalDays)
		}
		ta.Densities = append(ta.Densities, PoopDensity{
			Label:       ordinalLabel(ordinal),
			Probability: math.Round(prob*100) / 100,
			Curve:       scaledKDE(obs, prob),
		})
	}
	return ta, nil
}

// trackedDayWeights returns a recency weight per tracked day and their sum. A day
// counts only if it has real tracking — a classic (non-alone) session OR any
// simplified poop/nap/outing entry — so days with no tracking at all are excluded
// from the poop-probability denominator (they were never marked, just untracked).
// Weights decay from the most recent tracked day so recent behaviour dominates.
func trackedDayWeights(db *sql.DB) (map[string]float64, float64, error) {
	rows, err := db.Query(`
		SELECT DISTINCT date FROM sessions WHERE COALESCE(excluded,0)=0 AND COALESCE(alone,0)=0
		UNION SELECT DISTINCT day FROM poops   WHERE deleted_at IS NULL
		UNION SELECT DISTINCT day FROM naps    WHERE deleted_at IS NULL
		UNION SELECT DISTINCT day FROM outings WHERE deleted_at IS NULL
	`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var dates []string
	var ref time.Time
	for rows.Next() {
		var ds string
		if err := rows.Scan(&ds); err != nil {
			return nil, 0, err
		}
		d, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		dates = append(dates, ds)
		if d.After(ref) {
			ref = d
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	weights := make(map[string]float64, len(dates))
	var totalW float64
	for _, ds := range dates {
		d, _ := time.Parse("2006-01-02", ds)
		w := math.Exp2(-ref.Sub(d).Hours() / 24 / recencyHalfLifeDays)
		weights[ds] = w
		totalW += w
	}
	return weights, totalW, nil
}

// scaledKDE returns a 24-point circular Gaussian KDE of the weighted observations,
// normalised so the sum of the values (each a 1-hour bin) equals area.
func scaledKDE(obs []wobs, area float64) []float64 {
	kde := computeCircularKDE(obs)
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

// computeCircularKDE evaluates a recency-weighted Gaussian KDE at the midpoint of
// each hour, wrapping around midnight so 23:30 and 00:30 are treated as close.
func computeCircularKDE(obs []wobs) []float64 {
	const bandwidth = 1.2
	result := make([]float64, 24)
	for h := 0; h < 24; h++ {
		x := float64(h) + 0.5
		for _, o := range obs {
			d := x - o.h
			if d > 12 {
				d -= 24
			} else if d < -12 {
				d += 24
			}
			result[h] += o.w * math.Exp(-0.5*d*d/(bandwidth*bandwidth))
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

// weightedPercentile returns the p-quantile (0–1) of observations pre-sorted by
// hour, using cumulative weight: the quantile is where the running weight crosses
// p of the total, linearly interpolated between adjacent points.
func weightedPercentile(sorted []wobs, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0].h
	}
	total := 0.0
	for _, o := range sorted {
		total += o.w
	}
	if total == 0 {
		return sorted[len(sorted)/2].h
	}
	target := p * total
	cum := 0.0
	for i, o := range sorted {
		prev := cum
		cum += o.w
		if cum >= target {
			if i == 0 {
				return o.h
			}
			frac := 0.0
			if o.w > 0 {
				frac = (target - prev) / o.w
			}
			return sorted[i-1].h + frac*(o.h-sorted[i-1].h)
		}
	}
	return sorted[len(sorted)-1].h
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

	accidents, err := accidentTimes(db)
	if err != nil {
		return nil, err
	}
	accByWeek := make(map[int]int)
	for _, t := range accidents {
		accByWeek[ageWeek(t)]++
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
