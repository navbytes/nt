package task

// VisibleInList reports whether a task should appear in a default task listing,
// given the show-done / show-blocked toggles. It is the one visibility rule every
// surface shares (SPEC §9), extracted here so the CLI, TUI, web, and MCP can't
// drift on it:
//
//   - a done task hides unless includeDone;
//   - an open task that is dependency-blocked hides unless includeBlocked.
//
// blockedByDep is raw membership in the blocked set (see BlockedIDs) — the
// "& not-done" part is applied here so callers pass the set lookup directly.
func VisibleInList(t *Task, blockedByDep, includeDone, includeBlocked bool) bool {
	if t.Done && !includeDone {
		return false
	}
	if blockedByDep && !t.Done && !includeBlocked {
		return false
	}
	return true
}
