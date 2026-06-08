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
