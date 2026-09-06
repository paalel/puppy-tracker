// Package alone tracks unsupervised "alone time" — when the puppy is home alone
// and drifts in and out of sleep with no human driving the session state machine.
// It records a single open interval (pen_session) plus lightweight timestamped
// markers (pen_events), deliberately separate from the sessions table so it never
// affects the nap/settle/poop statistics.
package alone

import "time"

// Marker kinds. asleep/awake toggle the inferred rest state; restless is a
// momentary disturbance (treated as awake); toilet is transient and rest-neutral.
const (
	KindAsleep   = "asleep"
	KindAwake    = "awake"
	KindRestless = "restless"
	KindToilet   = "toilet"
)

func validKind(k string) bool {
	switch k {
	case KindAsleep, KindAwake, KindRestless, KindToilet:
		return true
	default:
		return false
	}
}

type Marker struct {
	Kind string
	At   time.Time
}

type Session struct {
	ID        int
	StartedAt time.Time
	EndedAt   *time.Time
	Markers   []Marker // ordered by time ascending
}

// asleepFraction estimates the share of the interval [StartedAt, end] spent
// asleep, walking the markers in order. State starts awake; an asleep marker
// begins a sleep stretch, awake/restless end it, toilet leaves it unchanged.
// end defaults to EndedAt, or now for a still-open session.
func (s Session) asleepFraction(now time.Time) float64 {
	end := now
	if s.EndedAt != nil {
		end = *s.EndedAt
	}
	total := end.Sub(s.StartedAt).Seconds()
	if total <= 0 {
		return 0
	}

	var asleepSecs float64
	cursor := s.StartedAt
	asleep := false
	for _, m := range s.Markers {
		if m.At.Before(cursor) || m.At.After(end) {
			continue
		}
		if asleep {
			asleepSecs += m.At.Sub(cursor).Seconds()
		}
		switch m.Kind {
		case KindAsleep:
			asleep = true
		case KindAwake, KindRestless:
			asleep = false
		}
		cursor = m.At
	}
	if asleep {
		asleepSecs += end.Sub(cursor).Seconds()
	}
	return asleepSecs / total
}

// count returns how many markers of a given kind the session holds.
func (s Session) count(kind string) int {
	n := 0
	for _, m := range s.Markers {
		if m.Kind == kind {
			n++
		}
	}
	return n
}
