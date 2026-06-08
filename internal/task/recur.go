package task

import (
	"strings"
	"time"
)

// SpawnNext builds the next occurrence of a recurring task (SPEC §9). The new
// task copies the description, priority, recurrence, source, and parent, gets a
// fresh ULID, and is scheduled by NextDue. Call it while the original is still
// open (before SetDone moves the priority to a pri: key). Returns nil if the
// recurrence spec is empty or unparseable (so no malformed duplicate spawns).
func SpawnNext(t *Task, today string) *Task {
	if _, _, _, ok := parseRec(t.Recur()); !ok {
		return nil
	}
	next := NextDue(t, today)
	n := New(t.Text)
	if t.Priority != 0 {
		n.SetPriority(t.Priority)
	}
	if next != "" {
		n.SetKey("due", next)
	}
	n.SetKey("rec", t.Recur())
	if s := t.Source(); s != "" {
		n.SetKey("src", s)
	}
	if p := t.Parent(); p != "" { // keep a recurring subtask attached to its parent
		n.SetKey("parent", p)
	}
	return n
}

// NextDue computes the next occurrence's due date for a recurring task, given
// the completion/reference date `today`:
//
//   - Strict recurrences (rec:+3d, rec:+weekly) anchor to the task's scheduled
//     due and roll FORWARD to the next slot on/after today — so a task completed
//     late never spawns an already-overdue occurrence, and the cadence stays
//     pinned to the calendar (e.g. "rent on the 1st").
//   - Plain recurrences (rec:3d, rec:weekly) schedule from `today` itself — the
//     "N days after I last did it" semantics (e.g. "water the plant 3d after").
//
// Returns "" when the task isn't recurring.
func NextDue(t *Task, today string) string {
	n, unit, strict, ok := parseRec(t.Recur())
	if !ok {
		return ""
	}
	if !strict {
		return shift(today, n, unit)
	}
	base := t.Due()
	if base == "" {
		base = today
	}
	next := shift(base, n, unit)
	for next != "" && next < today { // roll forward past missed occurrences
		adv := shift(next, n, unit)
		if adv == "" || adv == next {
			break
		}
		next = adv
	}
	return next
}

// AdvanceDue returns the task's due advanced to its next occurrence — one
// period past the current due (or today if undated), rolled forward to on/after
// today. Used by `nt skip` to move a recurring task forward without completing
// it. Returns "" if the task isn't recurring.
func AdvanceDue(t *Task, today string) string {
	n, unit, _, ok := parseRec(t.Recur())
	if !ok {
		return ""
	}
	base := t.Due()
	if base == "" {
		base = today
	}
	next := shift(base, n, unit)
	for next != "" && next < today {
		adv := shift(next, n, unit)
		if adv == "" || adv == next {
			break
		}
		next = adv
	}
	return next
}

// advance returns date shifted by ONE recurrence period, or date unchanged if
// either can't be parsed. (NextDue handles strict roll-forward; this is the
// single-step primitive, kept for direct date math.)
func advance(date, rec string) string {
	n, unit, _, ok := parseRec(rec)
	if !ok {
		return date
	}
	if out := shift(date, n, unit); out != "" {
		return out
	}
	return date
}

// shift moves an ISO date forward by n units (d/w/m/y), clamping month/year
// arithmetic to the last valid day so e.g. Jan 31 + 1 month is Feb 28/29 rather
// than silently overflowing into March (Go's AddDate normalizes otherwise).
func shift(date string, n int, unit byte) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	switch unit {
	case 'd':
		d = d.AddDate(0, 0, n)
	case 'w':
		d = d.AddDate(0, 0, 7*n)
	case 'm':
		d = addMonthsClamped(d, n)
	case 'y':
		d = addMonthsClamped(d, 12*n)
	}
	return d.Format("2006-01-02")
}

// addMonthsClamped adds months (months ≥ 0) and clamps the day to the last day
// of the target month.
func addMonthsClamped(d time.Time, months int) time.Time {
	y, mo, day := d.Date()
	total := int(mo) - 1 + months
	ty := y + total/12
	tm := time.Month(total%12 + 1)
	if last := daysInMonth(ty, tm); day > last {
		day = last
	}
	return time.Date(ty, tm, day, 0, 0, 0, 0, d.Location())
}

// daysInMonth returns the number of days in the given month (day 0 of the next
// month is the last day of this one).
func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// parseRec parses recurrence specs: the words daily/weekly/monthly/yearly, or
// N + unit where unit is d/w/m/y (e.g. "3d", "2w"); a leading "P" is tolerated
// (so "P3D" works). A leading "+" marks a STRICT (calendar-anchored) recurrence.
// The count defaults to 1 when omitted. Returns (count, unit, strict, ok).
func parseRec(rec string) (int, byte, bool, bool) {
	r := strings.ToLower(strings.TrimSpace(rec))
	strict := false
	if strings.HasPrefix(r, "+") {
		strict = true
		r = r[1:]
	}
	r = strings.TrimPrefix(r, "p")
	switch r {
	case "daily":
		return 1, 'd', strict, true
	case "weekly":
		return 1, 'w', strict, true
	case "monthly":
		return 1, 'm', strict, true
	case "yearly", "annually":
		return 1, 'y', strict, true
	case "":
		return 0, 0, false, false
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
		return 0, 0, false, false
	}
	switch r[i] {
	case 'd', 'w', 'm', 'y':
		return n, r[i], strict, true
	}
	return 0, 0, false, false
}
