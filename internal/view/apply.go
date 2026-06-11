package view

import (
	"sort"

	"github.com/navbytes/nt/internal/task"
)

// Apply selects and orders tasks per spec — the one shared implementation
// behind `nt list`, `nt view recall`, and the web's saved views, so the same
// named view can never filter differently on different surfaces (the drift the
// core audit warned about). blocked is the dependency-blocked id set
// (task.BlockedIDs) computed over the full document.
func Apply(tasks []*task.Task, spec Spec, blocked map[string]bool) []*task.Task {
	var rows []*task.Task
	for _, t := range tasks {
		if Keep(t, spec, blocked) {
			rows = append(rows, t)
		}
	}
	SortTasks(rows, spec.Sort)
	return rows
}

// Keep reports whether one task is selected by spec. An explicit Status matches
// the task's effective status (a dependency-blocked open task is "blocked");
// otherwise the default-list visibility rule applies: done hides unless All,
// dependency-blocked hides unless All/ShowBlocked.
func Keep(t *task.Task, spec Spec, blocked map[string]bool) bool {
	blockedByDep := blocked[t.ID()]
	if spec.Status != "" {
		if task.EffectiveStatus(t, blockedByDep && !t.Done) != spec.Status {
			return false
		}
	} else if !task.VisibleInList(t, blockedByDep, spec.All, spec.All || spec.ShowBlocked) {
		return false
	}
	if spec.Tag != "" && !containsStr(t.Tags(), spec.Tag) {
		return false
	}
	if spec.Project != "" && !containsStr(t.Projects(), spec.Project) {
		return false
	}
	return true
}

// SortTasks orders rows in place by the named sort: "urgency", "due" (no due
// date last), or "created". Anything else (including "") keeps file order.
func SortTasks(rows []*task.Task, by string) {
	switch by {
	case "urgency":
		task.SortByUrgency(rows)
	case "due":
		sort.SliceStable(rows, func(i, j int) bool { return dueKey(rows[i]) < dueKey(rows[j]) })
	case "created":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created < rows[j].Created })
	}
}

func dueKey(t *task.Task) string {
	if d := t.Due(); d != "" {
		return d
	}
	return "9999-99-99" // no due date sorts last
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
