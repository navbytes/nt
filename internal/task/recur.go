package task

import (
	"strings"
	"time"
)

// SpawnNext builds the next occurrence of a recurring task (SPEC §9). The new
// task copies the description, priority, recurrence, and source, gets a fresh
// ULID, and advances the due date by the recurrence period. Call it while the
// original is still open (before SetDone moves the priority to a pri: key).
func SpawnNext(t *Task, today string) *Task {
	base := t.Due()
	if base == "" {
		base = today
	}
	n := New(t.Text)
	if t.Priority != 0 {
		n.SetPriority(t.Priority)
	}
	if next := advance(base, t.Recur()); next != "" {
		n.SetKey("due", next)
	}
	n.SetKey("rec", t.Recur())
	if s := t.Source(); s != "" {
		n.SetKey("src", s)
	}
	return n
}

// advance returns date shifted by the recurrence spec, or date unchanged if
// either can't be parsed.
func advance(date, rec string) string {
	n, unit, ok := parseRec(rec)
	if !ok {
		return date
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	switch unit {
	case 'd':
		d = d.AddDate(0, 0, n)
	case 'w':
		d = d.AddDate(0, 0, 7*n)
	case 'm':
		d = d.AddDate(0, n, 0)
	case 'y':
		d = d.AddDate(n, 0, 0)
	}
	return d.Format("2006-01-02")
}

// parseRec parses recurrence specs: the words daily/weekly/monthly/yearly, or
// N + unit where unit is d/w/m/y (e.g. "3d", "2w"); a leading "P" is tolerated
// (so "P3D" works). The count defaults to 1 when omitted.
func parseRec(rec string) (int, byte, bool) {
	r := strings.ToLower(strings.TrimSpace(rec))
	r = strings.TrimPrefix(r, "p")
	switch r {
	case "daily":
		return 1, 'd', true
	case "weekly":
		return 1, 'w', true
	case "monthly":
		return 1, 'm', true
	case "yearly", "annually":
		return 1, 'y', true
	case "":
		return 0, 0, false
	}
	i, n := 0, 0
	for i < len(r) && r[i] >= '0' && r[i] <= '9' {
		n = n*10 + int(r[i]-'0')
		i++
	}
	if i == 0 {
		n = 1
	}
	if i >= len(r) {
		return 0, 0, false
	}
	switch r[i] {
	case 'd', 'w', 'm', 'y':
		return n, r[i], true
	}
	return 0, 0, false
}
