package stats

import (
	"database/sql"
	"math"
	"strings"
	"time"

	"puppy/store"
)

const (
	minKDESamples = 5
	kdeBandwidth  = 1.5
)

// lastAccidentBefore is the hardcoded baseline until a new accident is logged in the DB.
// Her last accident was 2026-06-20 14:00 CEST = 12:00 UTC.
var lastAccidentBefore = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

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
		WHERE slept_at IS NOT NULL AND excluded = 0
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
			WHERE s1.slept_at IS NOT NULL AND s1.excluded = 0 AND s2.excluded = 0 AND nap_secs > 0
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
			WHERE crate_at IS NOT NULL AND slept_at IS NOT NULL AND excluded = 0 AND settle_secs > 0
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
		  AND date NOT IN (SELECT DISTINCT date FROM sessions WHERE excluded = 1)
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

func getSessionSeries(db *sql.DB) (*SessionSeries, error) {
	rows, err := db.Query(`
		SELECT
			woke_at,
			crate_at,
			slept_at,
			COALESCE(sleep_ease, ''),
			CAST((strftime('%s', slept_at) - strftime('%s', woke_at)) / 60 AS INTEGER),
			CASE WHEN crate_at IS NOT NULL
			     THEN CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER)
			     ELSE NULL END,
			CASE WHEN LEAD(date) OVER (ORDER BY id) = date
			     THEN CAST((strftime('%s', LEAD(woke_at) OVER (ORDER BY id)) - strftime('%s', slept_at)) / 60 AS INTEGER)
			     ELSE NULL END
		FROM sessions
		WHERE woke_at IS NOT NULL AND slept_at IS NOT NULL AND excluded = 0
		  AND date >= date('now', '-30 days')
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s := &SessionSeries{}
	for rows.Next() {
		var wokeAt, sleptAt, sleepEase string
		var crateAt sql.NullString
		var awakeMins int
		var settleMins, napMins sql.NullInt64
		if err := rows.Scan(&wokeAt, &crateAt, &sleptAt, &sleepEase, &awakeMins, &settleMins, &napMins); err != nil {
			return nil, err
		}
		s.Awake = append(s.Awake, ChartPoint{X: strings.Replace(wokeAt, " ", "T", 1), Y: awakeMins})
		if settleMins.Valid {
			p := ChartPoint{X: strings.Replace(crateAt.String, " ", "T", 1), Y: int(settleMins.Int64)}
			switch sleepEase {
			case "easy":
				s.SettleEasy = append(s.SettleEasy, p)
			case "ok":
				s.SettleOk = append(s.SettleOk, p)
			case "hard":
				s.SettleHard = append(s.SettleHard, p)
			default:
				s.SettleNone = append(s.SettleNone, p)
			}
		}
		if napMins.Valid {
			s.Nap = append(s.Nap, ChartPoint{X: strings.Replace(sleptAt, " ", "T", 1), Y: int(napMins.Int64)})
			if t, err := parseTimestamp(sleptAt); err == nil {
				lt := t.Local()
				s.NapByTime = append(s.NapByTime, NumericPoint{
					X: float64(lt.Hour()) + float64(lt.Minute())/60.0,
					Y: int(napMins.Int64),
				})
			}
		}
	}
	return s, rows.Err()
}

func getAccidentFreeDays(db *sql.DB) (int, error) {
	var s string
	err := db.QueryRow(`
		SELECT woke_at FROM sessions
		WHERE toilet_accident = 1 AND woke_at IS NOT NULL
		ORDER BY woke_at DESC LIMIT 1
	`).Scan(&s)

	var since time.Time
	if err == sql.ErrNoRows {
		since = lastAccidentBefore
	} else if err != nil {
		return 0, err
	} else if t, err := parseTimestamp(s); err == nil {
		since = t
	}

	days := int(time.Since(since).Hours() / 24)
	return days, nil
}

func getToiletAnalytics(db *sql.DB) (*ToiletAnalytics, error) {
	rows, err := db.Query(`
		SELECT woke_at, toilet_poop FROM sessions WHERE woke_at IS NOT NULL ORDER BY woke_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := make([]int, 24)
	opportunities := make([]int, 24)
	poopCounts := make([]int, 24)
	var poopTimes []float64

	for rows.Next() {
		var s string
		var poopInt int
		if err := rows.Scan(&s, &poopInt); err != nil {
			return nil, err
		}
		t, err := parseTimestamp(s)
		if err != nil {
			continue
		}
		lt := t.Local()
		h := lt.Hour()
		opportunities[h]++
		if poopInt == 1 {
			buckets[h]++
			poopCounts[h]++
			poopTimes = append(poopTimes, float64(lt.Hour())+float64(lt.Minute())/60.0)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	nrows, err := db.Query(`SELECT occurred_at, toilet_poop FROM night_toilets ORDER BY occurred_at ASC`)
	if err != nil {
		return nil, err
	}
	defer nrows.Close()
	for nrows.Next() {
		var s string
		var poopInt int
		if err := nrows.Scan(&s, &poopInt); err != nil {
			return nil, err
		}
		t, err := parseTimestamp(s)
		if err != nil {
			continue
		}
		lt := t.Local()
		h := lt.Hour()
		opportunities[h]++
		if poopInt == 1 {
			buckets[h]++
			poopCounts[h]++
			poopTimes = append(poopTimes, float64(lt.Hour())+float64(lt.Minute())/60.0)
		}
	}
	if err := nrows.Err(); err != nil {
		return nil, err
	}

	totalWakes := 0
	for _, v := range opportunities {
		totalWakes += v
	}

	ta := &ToiletAnalytics{
		Buckets:    buckets,
		TotalPoops: len(poopTimes),
		TotalWakes: totalWakes,
	}

	if len(poopTimes) >= minKDESamples {
		ta.KDE = computeCircularKDE(poopTimes)
		maxBar, maxKDE := 0, 0.0
		for _, v := range buckets {
			if v > maxBar {
				maxBar = v
			}
		}
		for _, v := range ta.KDE {
			if v > maxKDE {
				maxKDE = v
			}
		}
		if maxKDE > 0 && maxBar > 0 {
			scale := float64(maxBar) / maxKDE
			for i := range ta.KDE {
				ta.KDE[i] *= scale
			}
		}
	}

	return ta, nil
}

// computeCircularKDE evaluates a Gaussian KDE at the midpoint of each hour,
// wrapping around midnight so 23:30 and 00:30 are treated as close.
func computeCircularKDE(times []float64) []float64 {
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
			result[h] += math.Exp(-0.5 * d * d / (kdeBandwidth * kdeBandwidth))
		}
	}
	return result
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
