package task

// BlockedIDs returns the set of task ULIDs that are blocked by an open task
// (SPEC §9). A task B with `blocks:X` blocks task X for as long as B is not
// done; X is then hidden from default listings.
func BlockedIDs(tasks []*Task) map[string]bool {
	blocked := map[string]bool{}
	for _, b := range tasks {
		if b.Done {
			continue
		}
		if tgt := b.Blocks(); tgt != "" {
			blocked[tgt] = true
		}
	}
	return blocked
}

// EffectiveStatus is the display status accounting for dependency blocking: a
// not-done task that is the target of an open blocker shows as "blocked" even
// without an explicit s:blocked marker.
func EffectiveStatus(t *Task, isBlocked bool) string {
	if t.Done {
		return "done"
	}
	if isBlocked || t.State() == "blocked" {
		return "blocked"
	}
	if t.State() == "doing" {
		return "doing"
	}
	return "open"
}
