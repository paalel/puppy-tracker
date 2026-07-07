package config

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

func Get(db *sql.DB) (*Config, error) {
	rows, err := db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	c := &Config{PuppyName: "Nova", AwakeMinutes: 40, NapMinutes: 90, WindDownMinutes: 25}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		switch k {
		case "puppy_name":
			if v != "" {
				c.PuppyName = v
			}
		case "awake_minutes":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.AwakeMinutes = n
			}
		case "nap_minutes":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.NapMinutes = n
			}
		case "wind_down_minutes":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.WindDownMinutes = n
			}
		case "puppy_birthdate":
			if t, err := time.Parse("2006-01-02", v); err == nil {
				c.Birthdate = &t
			}
		case "ntfy_topic":
			c.NtfyTopic = v
		}
	}
	return c, rows.Err()
}

func Save(db *sql.DB, c *Config) error {
	var birthdateStr string
	if c.Birthdate != nil {
		birthdateStr = c.Birthdate.Format("2006-01-02")
	}
	pairs := [][2]string{
		{"puppy_name", c.PuppyName},
		{"puppy_birthdate", birthdateStr},
		{"awake_minutes", strconv.Itoa(c.AwakeMinutes)},
		{"nap_minutes", strconv.Itoa(c.NapMinutes)},
		{"wind_down_minutes", strconv.Itoa(c.WindDownMinutes)},
	}
	for _, kv := range pairs {
		_, err := db.Exec(
			`INSERT INTO config (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			kv[0], kv[1],
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func EnsureNtfyTopic(db *sql.DB) error {
	cfg, err := Get(db)
	if err != nil {
		return err
	}
	if cfg.NtfyTopic != "" {
		return nil
	}
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	topic := fmt.Sprintf("puppy-%x", b)
	_, err = db.Exec(
		`INSERT INTO config (key, value) VALUES ('ntfy_topic', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		topic,
	)
	return err
}
