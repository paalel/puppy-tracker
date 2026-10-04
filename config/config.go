package config

import "time"

// Mode selects which version of the app renders: the full "classic" puppy
// tracker, or the pared-down "simplified" grown-dog tool.
type Mode string

const (
	ModeClassic    Mode = "classic"
	ModeSimplified Mode = "simplified"
)

type Config struct {
	PuppyName       string
	Birthdate       *time.Time
	AwakeMinutes    int
	NapMinutes      int
	WindDownMinutes int
	FirstWakeTime   string // "HH:MM", local time
	Mode            Mode
}
