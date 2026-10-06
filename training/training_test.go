package training

import "testing"

func TestWeekStart(t *testing.T) {
	cases := map[string]string{
		"2026-10-06": "2026-10-05", // Tue → Mon
		"2026-10-05": "2026-10-05", // Mon → itself
		"2026-10-11": "2026-10-05", // Sun → preceding Mon
		"2026-10-04": "2026-09-28", // Sun (prev week)
	}
	for in, want := range cases {
		if got := weekStart(in); got != want {
			t.Errorf("weekStart(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestValidQuality(t *testing.T) {
	for _, q := range Qualities {
		if !validQuality(q) {
			t.Errorf("%q should be valid", q)
		}
	}
	for _, q := range []string{"", "great", "meh"} {
		if validQuality(q) {
			t.Errorf("%q should be invalid", q)
		}
	}
}
