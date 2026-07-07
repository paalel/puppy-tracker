package routine

import "strings"

type RoutineSession struct {
	ID         int
	Position   int
	Label      string
	Activities []string
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
