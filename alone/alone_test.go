package alone

import (
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 1, 15, h, m, 0, 0, time.UTC) }

func TestAsleepFraction(t *testing.T) {
	end := at(11, 0)
	cases := []struct {
		name    string
		markers []Marker
		want    float64
	}{
		{"no markers → awake whole time", nil, 0},
		{
			"asleep half, then awake",
			[]Marker{{KindAsleep, at(10, 15)}, {KindAwake, at(10, 45)}},
			0.5, // 30 of 60 min
		},
		{
			"asleep to end (still resting)",
			[]Marker{{KindAsleep, at(10, 30)}},
			0.5, // 30 of 60 min
		},
		{
			"restless ends a sleep stretch",
			[]Marker{{KindAsleep, at(10, 0)}, {KindRestless, at(10, 20)}},
			20.0 / 60.0,
		},
		{
			"toilet does not change rest state",
			[]Marker{{KindAsleep, at(10, 0)}, {KindToilet, at(10, 30)}},
			1.0, // asleep the whole hour; toilet is rest-neutral
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Session{StartedAt: at(10, 0), EndedAt: &end, Markers: c.markers}
			got := s.asleepFraction(at(12, 0)) // now is ignored because EndedAt is set
			if diff := got - c.want; diff > 0.001 || diff < -0.001 {
				t.Errorf("asleepFraction = %.4f, want %.4f", got, c.want)
			}
		})
	}
}

func TestAsleepFractionOpenSessionUsesNow(t *testing.T) {
	s := Session{StartedAt: at(10, 0), Markers: []Marker{{KindAsleep, at(10, 0)}}}
	if got := s.asleepFraction(at(11, 0)); got != 1.0 {
		t.Errorf("open session asleep whole hour = %.4f, want 1.0", got)
	}
}

func TestCount(t *testing.T) {
	s := Session{Markers: []Marker{
		{KindRestless, at(10, 0)}, {KindRestless, at(10, 5)}, {KindToilet, at(10, 10)},
	}}
	if n := s.count(KindRestless); n != 2 {
		t.Errorf("restless count = %d, want 2", n)
	}
	if n := s.count(KindToilet); n != 1 {
		t.Errorf("toilet count = %d, want 1", n)
	}
}
