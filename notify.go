package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func sendNtfyNotification(topic, title, message string) error {
	req, err := http.NewRequest("POST", "https://ntfy.sh/"+topic, strings.NewReader(message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned %d", resp.StatusCode)
	}
	return nil
}

func startNotificationWorker(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cfg, err := getConfig(db)
			if err != nil || cfg.NtfyTopic == "" {
				continue
			}
			ids, err := getSessionsNeedingNotification(db, cfg.WindDownMinutes)
			if err != nil {
				log.Printf("notification worker: %v", err)
				continue
			}
			for _, id := range ids {
				if err := markSessionNotified(db, id); err != nil {
					log.Printf("ntfy mark notified: %v", err)
					continue
				}
				title := "Time to wind down"
				body := fmt.Sprintf("%s has been awake for %d minutes", cfg.PuppyName, cfg.WindDownMinutes)
				if err := sendNtfyNotification(cfg.NtfyTopic, title, body); err != nil {
					log.Printf("ntfy send: %v", err)
				}
			}
		}
	}()
}
