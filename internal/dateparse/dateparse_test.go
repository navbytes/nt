package dateparse

import (
	"testing"
	"time"
)

func TestPriority(t *testing.T) {
	cases := []struct {
		in   string
		want byte
		ok   bool
	}{
		{"high", 'A', true}, {"h", 'A', true}, {"a", 'A', true}, {"A", 'A', true},
		{"med", 'B', true}, {"medium", 'B', true}, {"m", 'B', true}, {"b", 'B', true},
		{"low", 'C', true}, {"l", 'C', true}, {"c", 'C', true},
		{"D", 'D', true}, {"z", 'Z', true}, // full A–Z range
		{"", 0, true}, {"none", 0, true}, {"-", 0, true}, // clear
		{"highest", 0, false}, {"1", 0, false}, {"ab", 0, false}, // invalid
	}
	for _, c := range cases {
		got, ok := Priority(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Priority(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestDate(t *testing.T) {
	now := time.Now()
	iso := func(d time.Time) string { return d.Format("2006-01-02") }

	if got, ok := Date("today"); !ok || got != iso(now) {
		t.Errorf("today = %q,%v", got, ok)
	}
	if got, ok := Date("tomorrow"); !ok || got != iso(now.AddDate(0, 0, 1)) {
		t.Errorf("tomorrow = %q,%v", got, ok)
	}
	if got, ok := Date("+3d"); !ok || got != iso(now.AddDate(0, 0, 3)) {
		t.Errorf("+3d = %q,%v", got, ok)
	}
	if got, ok := Date("2026-04-15"); !ok || got != "2026-04-15" {
		t.Errorf("ISO = %q,%v", got, ok)
	}
	if got, ok := Date(""); !ok || got != "" {
		t.Errorf("empty should clear: %q,%v", got, ok)
	}
	if _, ok := Date("someday"); ok {
		t.Error("unparseable date should fail")
	}
	// A weekday resolves to a future date (never today).
	if got, ok := Date("monday"); !ok || got <= iso(now) {
		t.Errorf("weekday should be in the future: %q,%v", got, ok)
	}
}

func TestDateWithTimeOfDay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-06-08T17:00", "2026-06-08T17:00"},
		{"2026-06-08 14:30", "2026-06-08T14:30"},
		{"2026-06-08 5pm", "2026-06-08T17:00"},
		{"2026-06-08 9am", "2026-06-08T09:00"},
		{"2026-06-08 12am", "2026-06-08T00:00"},
		{"2026-06-08 12pm", "2026-06-08T12:00"},
	}
	for _, c := range cases {
		if got, ok := Date(c.in); !ok || got != c.want {
			t.Errorf("Date(%q) = (%q,%v), want %q", c.in, got, ok, c.want)
		}
	}
	// Keyword + time.
	now := time.Now().Format("2006-01-02")
	if got, ok := Date("today 5pm"); !ok || got != now+"T17:00" {
		t.Errorf("today 5pm = %q,%v want %sT17:00", got, ok, now)
	}
	// A date with no time stays date-only.
	if got, _ := Date("2026-06-08"); got != "2026-06-08" {
		t.Errorf("date-only should stay date-only, got %q", got)
	}
	// Bad clock fails.
	if _, ok := Date("2026-06-08T99:99"); ok {
		t.Error("invalid clock should fail")
	}
}

func TestDatePart(t *testing.T) {
	if DatePart("2026-06-08T17:00") != "2026-06-08" {
		t.Error("DatePart should strip the time")
	}
	if DatePart("2026-06-08") != "2026-06-08" {
		t.Error("DatePart of a bare date is itself")
	}
}
