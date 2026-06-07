package mutate

import (
	"fmt"

	"github.com/navbytes/nt/internal/lock"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// DoctorReport summarizes what Doctor found (and, unless dry-run, fixed).
type DoctorReport struct {
	DupIDsRemoved int      // duplicate-ULID lines dropped
	IDsAssigned   int      // task lines that lacked an id and got one
	Actions       []string // human-readable, one per fix
}

// Issues is the total number of problems found.
func (r DoctorReport) Issues() int { return r.DupIDsRemoved + r.IDsAssigned }

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

	if rep.Issues() == 0 || !apply {
		return rep, nil
	}
	d.Nodes = kept
	return rep, store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644)
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
