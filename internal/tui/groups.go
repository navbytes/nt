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
	var visible []*task.Task
	for _, t := range tasks {
		// Shared done/blocked visibility rule (SPEC §9) — same as the CLI/web.
		if !task.VisibleInList(t, blocked[t.ID()], showDone, showBlocked) {
			continue
		}
		if !fuzzyMatch(t.Line(), filter) {
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

// buildLogbook groups the completed tasks by completion date (newest first) for
// the Logbook view, plus the flat selectable list. Selection/ordering is the
// domain rule (task.CompletedSince); this adapter adds only the interactive text
// filter and the human-friendly date headers. Scope is applied by the caller.
func buildLogbook(tasks []*task.Task, filter string) ([]group, []*task.Task) {
	var order []string
	buckets := map[string][]*task.Task{}
	for _, t := range task.CompletedSince(tasks, "") {
		if !fuzzyMatch(t.Line(), filter) {
			continue
		}
		k := t.Completed
		if _, ok := buckets[k]; !ok {
			order = append(order, k) // first-seen == date-desc (input is sorted)
		}
		buckets[k] = append(buckets[k], t)
	}
	var groups []group
	var flat []*task.Task
	for _, k := range order {
		groups = append(groups, group{name: logDateLabel(k), tasks: buckets[k]})
		flat = append(flat, buckets[k]...)
	}
	return groups, flat
}

// logDateLabel renders a friendly Logbook group header for a YYYY-MM-DD
// completion date: Today / Yesterday / weekday (within a week) / Mon Jan 2.
func logDateLabel(ymd string) string {
	if ymd == "" {
		return "Completion date unknown"
	}
	d, err := time.Parse("2006-01-02", ymd)
	if err != nil {
		return ymd
	}
	now := time.Now()
	switch ymd {
	case now.Format("2006-01-02"):
		return "Today · " + d.Format("Mon Jan 2")
	case now.AddDate(0, 0, -1).Format("2006-01-02"):
		return "Yesterday · " + d.Format("Mon Jan 2")
	}
	if ymd >= now.AddDate(0, 0, -7).Format("2006-01-02") {
		return d.Format("Monday · Jan 2")
	}
	return d.Format("Mon Jan 2, 2006")
}

// byDate buckets tasks into Overdue / Today / This week / Later / No date, with
// a trailing Done bucket for any completed tasks present.
func byDate(tasks []*task.Task) []group {
	buckets := map[string][]*task.Task{}
	today := time.Now().Format("2006-01-02")
	weekOut := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	for _, t := range tasks {
		// Shared planner bucketing (date-prefix aware) — same rule as the web.
		k := task.DueBucket(t, today, weekOut)
		buckets[k] = append(buckets[k], t)
	}
	var out []group
	for _, name := range task.DueBuckets {
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

func sortByUrgency(ts []*task.Task) { task.SortByUrgency(ts) }
