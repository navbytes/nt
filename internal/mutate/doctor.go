package mutate

import (
	"fmt"

	"github.com/navbytes/nt/internal/lock"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// DoctorReport summarizes what Doctor found (and, unless dry-run, fixed).
type DoctorReport struct {
	DupIDsRemoved int      // duplicate-ULID lines dropped (within tasks.txt)
	CrossFileDups int      // tasks dropped from tasks.txt because done.txt has them
	IDsAssigned   int      // task lines that lacked an id and got one
	Actions       []string // human-readable, one per fix
	Warnings      []string // structural problems doctor reports but can't auto-fix
}

// Issues is the count of auto-fixable problems (used to decide whether to write).
func (r DoctorReport) Issues() int { return r.DupIDsRemoved + r.CrossFileDups + r.IDsAssigned }

// HasProblems reports whether anything is wrong — fixable issues OR warnings —
// so `nt doctor --check` exits non-zero on a dependency cycle / dangling edge
// even when there's nothing to rewrite.
func (r DoctorReport) HasProblems() bool { return r.Issues() > 0 || len(r.Warnings) > 0 }

// Doctor reconciles tasks.txt after a git merge (or hand-editing): it drops
// duplicate-ULID lines that a `merge=union` merge can leave behind, and assigns
// ids to any task line missing one. Like Archive it runs under the lock and is
// intentionally NOT journaled — it's a repair, and git is the recovery path for
// a git-tracked store. With apply=false it reports without writing (dry-run).
//
// Duplicate resolution keeps one line per id, preferring a completed line so a
// "done on one branch" state is never lost.
func (e *Engine) Doctor(apply bool) (DoctorReport, error) {
	var rep DoctorReport

	h, err := lock.Acquire(e.S.LockFile(), lock.DefaultTimeout)
	if err != nil {
		return rep, err
	}
	defer h.Release()

	data, err := store.ReadFile(e.S.TasksFile())
	if err != nil {
		return rep, err
	}
	d := task.Parse(data)

	// 1. Assign ids to task lines that lack one (can't dedup or address them).
	for _, n := range d.Nodes {
		if n.Task != nil && n.Task.ID() == "" {
			rep.IDsAssigned++
			rep.Actions = append(rep.Actions, "assigned id  "+clip(n.Task.Text))
			if apply {
				n.Task.EnsureID()
			}
		}
	}

	// 2. Choose one keeper per id (prefer a completed line over an open one).
	winner := map[string]int{}
	for i, n := range d.Nodes {
		if n.Task == nil || n.Task.ID() == "" {
			continue
		}
		id := n.Task.ID()
		if w, seen := winner[id]; !seen {
			winner[id] = i
		} else if n.Task.Done && !d.Nodes[w].Task.Done {
			winner[id] = i
		}
	}

	// 3. Drop every task line that isn't its id's keeper.
	kept := d.Nodes[:0]
	for i, n := range d.Nodes {
		if n.Task != nil && n.Task.ID() != "" && winner[n.Task.ID()] != i {
			rep.DupIDsRemoved++
			rep.Actions = append(rep.Actions, fmt.Sprintf("dropped dup %s  %s", short(n.Task.ID()), clip(n.Task.Text)))
			continue
		}
		kept = append(kept, n)
	}

	// 3b. Cross-file reconciliation: a task present in BOTH tasks.txt and done.txt
	// is a crash-leftover from Archive, which appends to done.txt then rewrites
	// tasks.txt in two steps — a crash between them duplicates the task across
	// files. done.txt is authoritative (written first), so drop the tasks.txt copy.
	if doneData, derr := store.ReadFile(e.S.DoneFile()); derr == nil && len(doneData) > 0 {
		archived := map[string]bool{}
		for _, n := range task.Parse(doneData).Nodes {
			if n.Task != nil && n.Task.ID() != "" {
				archived[n.Task.ID()] = true
			}
		}
		pruned := kept[:0]
		for _, n := range kept {
			if n.Task != nil && n.Task.ID() != "" && archived[n.Task.ID()] {
				rep.CrossFileDups++
				rep.Actions = append(rep.Actions, fmt.Sprintf("dropped archived dup %s  %s", short(n.Task.ID()), clip(n.Task.Text)))
				continue
			}
			pruned = append(pruned, n)
		}
		kept = pruned
	}

	// 4. Dependency-integrity warnings (not auto-fixable — breaking a cycle or a
	// stale edge is a user decision). Computed on the post-dedup task set.
	checkNodes := kept
	if !apply {
		checkNodes = d.Nodes // dry-run hasn't pruned dups; check the keepers only
	}
	var checkTasks []*task.Task
	seen := map[string]bool{}
	for _, n := range checkNodes {
		if n.Task != nil && n.Task.ID() != "" && !seen[n.Task.ID()] {
			seen[n.Task.ID()] = true
			checkTasks = append(checkTasks, n.Task)
		}
	}
	for _, cyc := range task.DepCycles(checkTasks) {
		shorts := make([]string, len(cyc))
		for i, id := range cyc {
			shorts[i] = short(id)
		}
		rep.Warnings = append(rep.Warnings, "dependency cycle: "+joinArrow(shorts))
	}
	for _, id := range task.DanglingBlocks(checkTasks) {
		rep.Warnings = append(rep.Warnings, "dangling blocks: target on task "+short(id)+" no longer exists")
	}

	if rep.Issues() == 0 || !apply {
		return rep, nil
	}
	d.Nodes = kept
	return rep, store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644)
}

func joinArrow(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " → "
		}
		out += id
	}
	if len(ids) > 0 {
		out += " → " + ids[0] // close the loop visually
	}
	return out
}

func short(id string) string {
	if len(id) > 6 {
		return id[len(id)-6:]
	}
	return id
}

func clip(s string) string {
	const max = 50
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
