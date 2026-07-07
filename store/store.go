package store

import "time"

const (
	WakeRolloverHour = 4 // wakes before this hour belong to the previous calendar day
	DateFormat       = "2006-01-02"
	TimestampFormat  = "2006-01-02 15:04:05"
)

// ParseTimestamp parses a UTC timestamp from SQLite storage format or RFC3339.
func ParseTimestamp(s string) (time.Time, error) {
	t, err := time.ParseInLocation(TimestampFormat, s, time.UTC)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// ParseDate parses a SQLite date string.
func ParseDate(s string) (time.Time, error) {
	return time.Parse(DateFormat, s)
}

// NowUTC returns the current time formatted for SQLite timestamp storage.
func NowUTC() string {
	return time.Now().UTC().Format(TimestampFormat)
}

// Today returns the current local date as a SQLite date string.
func Today() string {
	return time.Now().Format(DateFormat)
}

// FormatDate formats a time as a SQLite date string.
func FormatDate(t time.Time) string {
	return t.Format(DateFormat)
}

// FormatTimestamp formats a time as a SQLite timestamp string in UTC.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(TimestampFormat)
}
