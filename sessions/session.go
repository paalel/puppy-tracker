package sessions

import "time"

type Phase string

const (
	PhaseActive   Phase = "ACTIVE"
	PhaseCrating  Phase = "CRATING"
	PhaseSleeping Phase = "SLEEPING"
	PhaseAlone    Phase = "ALONE"
)

// Home-alone quality values.
const (
	SleptWell = "well"
	SleptSome = "some"
	SleptNone = "none"

	BehaviourCalm      = "calm"
	BehaviourUnsettled = "unsettled"
	BehaviourStressed  = "stressed"

	LocationCage    = "cage"
	LocationPen     = "pen"
	LocationRoaming = "roaming"
)

type puppyState struct {
	Phase          Phase
	PhaseStartedAt time.Time
}

type dbSession struct {
	ID                    int
	RoutineSessionID      *int
	WokeAt                *time.Time
	CrateAt               *time.Time
	SleptAt               *time.Time
	Comment               string
	SleepEase             string
	Overtired             bool
	ToiletPee             bool
	ToiletPoop            bool
	ToiletAccident        bool
	TrainingQuality       string
	PhysicalActivity      bool
	MentalActivity        bool
	CalmWinddown          bool
	EnvironmentalActivity bool
	Excluded              bool
	Alone                 bool
	AloneSlept            string
	AloneBehaviour        string
	AloneLocation         string
	AloneDestroyed        bool
}

type SessionView struct {
	ID                    int
	Index                 int
	Position              int
	Label                 string
	Activities            []string
	PlannedWake           time.Time
	PlannedSleep          time.Time
	ActualWake            *time.Time
	ActualSleep           *time.Time
	IsPast                bool
	IsActive              bool
	IsFuture              bool
	ActualDuration        string
	DurationClass         string
	Comment               string
	SleepEase             string
	Overtired             bool
	ToiletPee             bool
	ToiletPoop            bool
	ToiletAccident        bool
	TrainingQuality       string
	PhysicalActivity      bool
	MentalActivity        bool
	CalmWinddown          bool
	EnvironmentalActivity bool
	ActualCrate           *time.Time
	SleepDuration         string
	SettleDuration        string
	Excluded              bool
	Alone                 bool
	AloneSlept            string
	AloneBehaviour        string
	AloneLocation         string
	AloneDestroyed        bool
	PoopLikelihood        float64
	PoopLo                float64
	PoopHi                float64
}

type PoopStatus struct {
	LastPoop *time.Time
}
