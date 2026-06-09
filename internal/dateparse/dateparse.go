// Package dateparse parses the human date and priority inputs nt accepts on the
// CLI and in the TUI (SPEC §7.3), kept in one place so both surfaces agree.
package dateparse

import (
	"fmt"
	"regexp"
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

var isoDateTimeRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[ tT](\d{1,2}:\d{2})$`)

// Date accepts today, tomorrow, weekday names, +Nd, and YYYY-MM-DD, optionally
// with a time-of-day: an ISO "YYYY-MM-DD[T ]HH:MM", or a clock appended to any
// of the keyword forms ("today 5pm", "fri 17:00", "tomorrow 9am"). A timed value
// is normalized to "YYYY-MM-DDTHH:MM". Empty / "none" / "-" clears the date
// (returns "", true). ok is false on an unparseable value.
func Date(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if l := strings.ToLower(s); l == "" || l == "none" || l == "-" {
		return "", true
	}
	// ISO date+time, e.g. "2026-06-08T17:00" or "2026-06-08 14:30".
	if m := isoDateTimeRe.FindStringSubmatch(s); m != nil {
		if clk, ok := parseClock(m[2]); ok {
			return m[1] + "T" + clk, true
		}
		return "", false
	}
	// "<date expr> <time expr>", e.g. "today 5pm", "fri 9:30am".
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		if clk, ok := parseClock(s[i+1:]); ok {
			if d, ok := dateOnly(s[:i]); ok && d != "" {
				return d + "T" + clk, true
			}
		}
	}
	return dateOnly(s)
}

// dateOnly resolves a date expression with no time component.
func dateOnly(s string) (string, bool) {
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

// parseClock parses a time-of-day — "HH:MM" (24h), "H:MMam/pm", or "Hpm" — into a
// normalized "HH:MM". ok is false if it isn't a clock.
func parseClock(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	ampm := ""
	switch {
	case strings.HasSuffix(s, "am"):
		ampm, s = "am", strings.TrimSpace(s[:len(s)-2])
	case strings.HasSuffix(s, "pm"):
		ampm, s = "pm", strings.TrimSpace(s[:len(s)-2])
	}
	h, m := 0, 0
	if i := strings.IndexByte(s, ':'); i >= 0 {
		var ok1, ok2 bool
		if h, ok1 = atoi(s[:i]); !ok1 {
			return "", false
		}
		if m, ok2 = atoi(s[i+1:]); !ok2 {
			return "", false
		}
	} else {
		var ok bool
		if h, ok = atoi(s); !ok {
			return "", false
		}
	}
	if ampm == "pm" && h < 12 {
		h += 12
	}
	if ampm == "am" && h == 12 {
		h = 0
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, m), true
}

// atoi parses a non-negative integer; ok is false on any non-digit.
func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// DatePart returns the YYYY-MM-DD prefix of a (possibly time-bearing) date value,
// for date-granularity comparisons (agenda buckets, overdue checks).
func DatePart(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// Priority maps a priority input to a todo.txt priority byte. It accepts the
// friendly aliases high/med/low, and any single letter A–Z (todo.txt's full
// priority range, e.g. "D"). "none"/"" clears it (returns 0, true).
func Priority(s string) (byte, bool) {
	t := strings.TrimSpace(s)
	switch strings.ToLower(t) {
	case "", "none", "-":
		return 0, true
	case "high", "h":
		return 'A', true
	case "med", "medium", "m":
		return 'B', true
	case "low", "l":
		return 'C', true
	}
	// Any single A–Z letter is a literal todo.txt priority.
	if len(t) == 1 {
		c := t[0]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c >= 'A' && c <= 'Z' {
			return c, true
		}
	}
	return 0, false
}

// Duration parses a human time estimate/elapsed into whole minutes: "90m", "2h",
// "1h30m", "1.5h", or a bare integer (minutes). ok is false on an unparseable or
// negative value. Used for task est:/spent: tracking (T6).
func Duration(s string) (int, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, false
	}
	if n, ok := atoi(s); ok { // bare integer = minutes
		return n, true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, false
	}
	return int(d.Minutes()), true
}

// FmtDuration renders whole minutes as a compact "1h30m" / "2h" / "45m".
func FmtDuration(mins int) string {
	if mins <= 0 {
		return "0m"
	}
	h, m := mins/60, mins%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}
