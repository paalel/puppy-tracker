package routine

import (
	"fmt"
	"strings"
)

type RoutineSession struct {
	ID         int
	Position   int
	Label      string
	Activities []string
}

// SessionView wraps RoutineSession with computed planned wake/sleep times for display.
type SessionView struct {
	RoutineSession
	PlannedWake  string
	PlannedSleep string
}

// SectionData is passed to the routine-sessions template fragment.
type SectionData struct {
	Sessions      []SessionView
	TotalAwakeStr string
	TotalSleepStr string
}

// BuildSection computes session views and daily totals for the settings page.
func BuildSection(sessions []RoutineSession, firstWakeTime string, awakeMins, napMins int) SectionData {
	var baseH, baseM int
	fmt.Sscanf(firstWakeTime, "%d:%02d", &baseH, &baseM)
	baseMinutes := baseH*60 + baseM
	cycle := awakeMins + napMins

	views := make([]SessionView, len(sessions))
	for i, s := range sessions {
		wake := baseMinutes + i*cycle
		sleep := wake + awakeMins
		views[i] = SessionView{
			RoutineSession: s,
			PlannedWake:    fmt.Sprintf("~%02d:%02d", (wake/60)%24, wake%60),
			PlannedSleep:   fmt.Sprintf("~%02d:%02d", (sleep/60)%24, sleep%60),
		}
	}

	totalAwake := len(sessions) * awakeMins
	totalSleep := 24*60 - totalAwake
	return SectionData{
		Sessions:      views,
		TotalAwakeStr: fmtDuration(totalAwake),
		TotalSleepStr: fmtDuration(totalSleep),
	}
}

func fmtDuration(mins int) string {
	if mins <= 0 {
		return "0m"
	}
	h, m := mins/60, mins%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func splitActivities(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func JoinActivities(acts []string) string {
	return strings.Join(acts, "\n")
}
