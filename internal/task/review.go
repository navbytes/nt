package task

import (
	"sort"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/ulid"
)

// Review is the weekly-triage breakdown surfaced by `nt review` (CLI) and the
// web /review: what needs a decision. Shared here so both surfaces bucket
// identically (SPEC §9, the shared query layer).
type Review struct {
	Overdue       []*Task  // actionable, past due
	Stale         []*Task  // open ≥ StaleDays, not overdue, no progress
	Undated       []*Task  // actionable, no due date
	StuckProjects []string // projects whose every open task is blocked
	StaleDays     int      // the threshold used, for display
}

// BuildReview triages tasks into the review buckets. today is ISO (YYYY-MM-DD);
// blocked maps task id → dependency-blocked (from BlockedIDs). A task appears in
// at most one task bucket (overdue wins over stale wins over undated).
func BuildReview(tasks []*Task, blocked map[string]bool, staleDays int, today string) Review {
	r := Review{StaleDays: staleDays}
	seen := map[string]bool{}
	put := func(set *[]*Task, t *Task) { *set = append(*set, t); seen[t.ID()] = true }

	// Overdue: actionable, past due.
	for _, t := range tasks {
		if t.Done || deferred(t, today) {
			continue
		}
		if due := dateparse.DatePart(t.Due()); due != "" && due < today {
			put(&r.Overdue, t)
		}
	}
	// Stale: actionable, not already shown, not in progress, older than the threshold.
	for _, t := range tasks {
		if t.Done || seen[t.ID()] || deferred(t, today) || t.State() == "doing" {
			continue
		}
		if ct, ok := ulid.Time(t.ID()); ok && int(time.Since(ct).Hours()/24) >= staleDays {
			put(&r.Stale, t)
		}
	}
	// Undated: actionable, no due date, not already shown.
	for _, t := range tasks {
		if t.Done || seen[t.ID()] || deferred(t, today) {
			continue
		}
		if t.Due() == "" {
			put(&r.Undated, t)
		}
	}
	r.StuckProjects = stuckProjects(tasks, blocked, today)
	return r
}

// deferred reports whether a task has a future start (t:) date and so isn't yet
// actionable.
func deferred(t *Task, today string) bool {
	s := dateparse.DatePart(t.Start())
	return s != "" && s > today
}

// stuckProjects returns projects whose every actionable (open, not deferred)
// task is dependency-blocked — there is no next action to take on them.
func stuckProjects(tasks []*Task, blocked map[string]bool, today string) []string {
	type stat struct{ open, blockedOpen int }
	stats := map[string]*stat{}
	for _, t := range tasks {
		if t.Done || deferred(t, today) {
			continue
		}
		for _, p := range t.Projects() {
			s := stats[p]
			if s == nil {
				s = &stat{}
				stats[p] = s
			}
			s.open++
			if blocked[t.ID()] {
				s.blockedOpen++
			}
		}
	}
	out := []string{} // non-nil so it serializes as [] not null (the web reads .length)
	for p, s := range stats {
		if s.open > 0 && s.open == s.blockedOpen {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
