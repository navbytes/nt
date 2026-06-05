package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/task"
)

// buildGroups buckets and orders tasks for display according to the grouping
// mode, the active filter, and whether done tasks are shown.
func buildGroups(tasks []*task.Task, grp groupMode, filter string, showDone, showBlocked bool, blocked map[string]bool) []group {
	needle := strings.ToLower(strings.TrimSpace(filter))
	var visible []*task.Task
	for _, t := range tasks {
		if t.Done && !showDone {
			continue
		}
		if blocked[t.ID()] && !t.Done && !showBlocked {
			continue // dependency-blocked tasks hide by default (SPEC §9)
		}
		if needle != "" && !strings.Contains(strings.ToLower(t.Line()), needle) {
			continue
		}
		visible = append(visible, t)
	}
	switch grp {
	case groupProject:
		return byKey(visible, func(t *task.Task) string {
			if p := t.Projects(); len(p) > 0 {
				return "+" + p[0]
			}
			return "(no project)"
		})
	case groupTag:
		return byKey(visible, func(t *task.Task) string {
			if tg := t.Tags(); len(tg) > 0 {
				return "@" + tg[0]
			}
			return "(untagged)"
		})
	default:
		return byDate(visible)
	}
}

// byDate buckets tasks into Overdue / Today / This week / Later / No date, with
// a trailing Done bucket for any completed tasks present.
func byDate(tasks []*task.Task) []group {
	order := []string{"Overdue", "Today", "This week", "Later", "No date", "Done"}
	buckets := map[string][]*task.Task{}
	today := time.Now().Format("2006-01-02")
	weekOut := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	for _, t := range tasks {
		var k string
		switch {
		case t.Done:
			k = "Done"
		case t.Due() == "":
			k = "No date"
		case t.Due() < today:
			k = "Overdue"
		case t.Due() == today:
			k = "Today"
		case t.Due() <= weekOut:
			k = "This week"
		default:
			k = "Later"
		}
		buckets[k] = append(buckets[k], t)
	}
	var out []group
	for _, name := range order {
		ts := buckets[name]
		if len(ts) == 0 {
			continue
		}
		sortByUrgency(ts)
		out = append(out, group{name: name, tasks: ts})
	}
	return out
}

// byKey groups tasks by an arbitrary key, sorting group names alphabetically
// but pushing the parenthesized "(none)" bucket last.
func byKey(tasks []*task.Task, key func(*task.Task) string) []group {
	buckets := map[string][]*task.Task{}
	for _, t := range tasks {
		k := key(t)
		buckets[k] = append(buckets[k], t)
	}
	names := make([]string, 0, len(buckets))
	for k := range buckets {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		pi, pj := strings.HasPrefix(names[i], "("), strings.HasPrefix(names[j], "(")
		if pi != pj {
			return pj // non-parenthesized first
		}
		return names[i] < names[j]
	})
	var out []group
	for _, name := range names {
		ts := buckets[name]
		sortByUrgency(ts)
		out = append(out, group{name: name, tasks: ts})
	}
	return out
}

func sortByUrgency(ts []*task.Task) {
	sort.SliceStable(ts, func(i, j int) bool { return urgency(ts[i]) > urgency(ts[j]) })
}

// urgency scores a task by priority, due-date proximity, and doing-state — the
// same shape as the CLI's --sort urgency (SPEC §9).
func urgency(t *task.Task) float64 {
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
				s += 12
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
