package task

import "sort"

// CompletedSince returns the completed tasks, newest completion first, optionally
// bounded to those completed on or after `since` (a YYYY-MM-DD date; "" = no
// bound). This is the core rule behind both the TUI Logbook and `nt log`, so the
// ordering and the meaning of "completed" live in the domain, not an adapter.
//
// A since bound excludes tasks with no completion date (an empty date sorts
// before any real one), which is the desired behaviour for "what did I finish in
// the last N days".
func CompletedSince(tasks []*Task, since string) []*Task {
	out := make([]*Task, 0, len(tasks))
	for _, t := range tasks {
		if !t.Done {
			continue
		}
		if since != "" && t.Completed < since {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Completed != out[j].Completed {
			return out[i].Completed > out[j].Completed // newest completion first
		}
		return out[i].Text < out[j].Text
	})
	return out
}
