// Package dateparse parses the human date and priority inputs nt accepts on the
// CLI and in the TUI (SPEC §7.3), kept in one place so both surfaces agree.
package dateparse

import (
	"strings"
	"time"
)

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

// Date accepts today, tomorrow, weekday names, +Nd, and YYYY-MM-DD. An empty
// string or "none"/"-" clears the date (returns "", true). ok is false on an
// unparseable value.
func Date(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	now := time.Now()
	switch s {
	case "", "none", "-":
		return "", true
	case "today":
		return now.Format("2006-01-02"), true
	case "tomorrow", "tom":
		return now.AddDate(0, 0, 1).Format("2006-01-02"), true
	}
	if wd, ok := weekdays[s]; ok {
		days := (int(wd) - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7 // next occurrence, not today
		}
		return now.AddDate(0, 0, days).Format("2006-01-02"), true
	}
	if strings.HasPrefix(s, "+") && strings.HasSuffix(s, "d") {
		body := s[1 : len(s)-1]
		if body == "" {
			return "", false
		}
		n := 0
		for _, c := range body {
			if c < '0' || c > '9' {
				return "", false
			}
			n = n*10 + int(c-'0')
		}
		return now.AddDate(0, 0, n).Format("2006-01-02"), true
	}
	if _, err := time.Parse("2006-01-02", s); err == nil {
		return s, true
	}
	return "", false
}

// Priority maps high/med/low (or A/B/C) to a priority byte. "none"/"" clears it
// (returns 0, true).
func Priority(s string) (byte, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "-":
		return 0, true
	case "high", "h", "a":
		return 'A', true
	case "med", "medium", "m", "b":
		return 'B', true
	case "low", "l", "c":
		return 'C', true
	}
	return 0, false
}
