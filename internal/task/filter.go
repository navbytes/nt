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

// Date buckets for the planner/agenda grouping, in display order.
const (
	BucketOverdue  = "Overdue"
	BucketToday    = "Today"
	BucketThisWeek = "This week"
	BucketLater    = "Later"
	BucketNoDate   = "No date"
	BucketDone     = "Done"
)

// DueBuckets is the bucket order for a date-grouped task view.
var DueBuckets = []string{BucketOverdue, BucketToday, BucketThisWeek, BucketLater, BucketNoDate, BucketDone}

// DueBucket assigns a task to a planner bucket relative to today and weekEnd
// (both YYYY-MM-DD). Comparisons use the date prefix, so a due with a time-of-day
// (due:…T17:00) still buckets by its calendar day — the rule the TUI and the web
// agenda share, kept here so they can't drift (and the TUI no longer mis-files a
// same-day timed due into "This week").
func DueBucket(t *Task, today, weekEnd string) string {
	switch {
	case t.Done:
		return BucketDone
	case t.Due() == "":
		return BucketNoDate
	}
	due := t.Due()
	if len(due) > 10 {
		due = due[:10] // ignore any time-of-day suffix
	}
	switch {
	case due < today:
		return BucketOverdue
	case due == today:
		return BucketToday
	case due <= weekEnd:
		return BucketThisWeek
	default:
		return BucketLater
	}
}
