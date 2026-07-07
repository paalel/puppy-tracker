package store

import "time"

// ParseTimestamp parses a UTC timestamp from SQLite storage format or RFC3339.
func ParseTimestamp(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// NowUTC returns the current time formatted for SQLite storage.
func NowUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
