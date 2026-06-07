package task

import (
	"sort"
	"time"
)

// Urgency scores a task by priority, due-date proximity, and doing-state. It's
// the single ranking behind `nt list --sort urgency`, the TUI's grouping, and
// `nt ready` / the MCP server, so the three never drift (SPEC §9).
func Urgency(t *Task) float64 {
	var s float64
	switch t.Priority {
	case 'A':
		s += 6
	case 'B':
		s += 4
	case 'C':
		s += 2
	}
	if due := t.Due(); due != "" {
		if dt, err := time.Parse("2006-01-02", due); err == nil {
			days := time.Until(dt).Hours() / 24
			switch {
			case days < 0:
				s += 12 // overdue
			case days < 1:
				s += 8
			case days < 3:
				s += 5
			case days < 7:
				s += 3
			}
		}
	}
	if t.State() == "doing" {
		s += 3
	}
	return s
}

// SortByUrgency orders tasks most-urgent first (stable).
func SortByUrgency(ts []*Task) {
	sort.SliceStable(ts, func(i, j int) bool { return Urgency(ts[i]) > Urgency(ts[j]) })
}
