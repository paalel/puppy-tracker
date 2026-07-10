package config

import "time"

type Config struct {
	PuppyName       string
	Birthdate       *time.Time
	AwakeMinutes    int
	NapMinutes      int
	WindDownMinutes int
}
