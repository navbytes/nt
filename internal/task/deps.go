package task

// BlockedIDs returns the set of task ULIDs that are blocked by an open task
// (SPEC §9). A task B with `blocks:X` blocks task X for as long as B is not
// done; X is then hidden from default listings. Tasks caught in a dependency
// cycle are deliberately NOT marked blocked — none could ever unblock, so
// hiding them all would be an invisible deadlock; they stay visible so the user
// can break the cycle (and `nt doctor` reports it).
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
	for _, cyc := range DepCycles(tasks) {
		for _, id := range cyc {
			delete(blocked, id)
		}
	}
	return blocked
}

// blocksEdges builds the active blocking graph: each open task's blocks: target
// that actually exists. The graph is functional (a task has at most one blocks:
// key, so out-degree ≤ 1), which the cycle walk relies on.
func blocksEdges(tasks []*Task) map[string]string {
	exists := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if id := t.ID(); id != "" {
			exists[id] = true
		}
	}
	next := map[string]string{}
	for _, t := range tasks {
		if t.Done {
			continue // a completed blocker no longer blocks
		}
		if tgt := t.Blocks(); tgt != "" && exists[tgt] {
			next[t.ID()] = tgt
		}
	}
	return next
}

// DepCycles returns each dependency cycle among open tasks as a list of task
// ULIDs (e.g. A blocks B, B blocks A → ["A","B"]). Because the blocking graph is
// functional, a cycle is found by following each chain until it revisits a node
// already on the current path.
func DepCycles(tasks []*Task) [][]string {
	next := blocksEdges(tasks)
	const (
		unseen = iota
		onPath
		settled
	)
	state := map[string]int{}
	var cycles [][]string
	for start := range next {
		if state[start] != unseen {
			continue
		}
		var path []string
		pos := map[string]int{}
		n, ok := start, true
		for ok && state[n] == unseen {
			state[n] = onPath
			pos[n] = len(path)
			path = append(path, n)
			n, ok = next[n]
		}
		if ok && state[n] == onPath { // closed a loop back onto the current path
			cycles = append(cycles, append([]string{}, path[pos[n]:]...))
		}
		for _, p := range path {
			state[p] = settled
		}
	}
	return cycles
}

// DanglingBlocks returns the ULIDs of tasks whose blocks: target no longer
// exists (e.g. the blocked task was deleted) — a stale edge `nt doctor` reports.
func DanglingBlocks(tasks []*Task) []string {
	exists := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if id := t.ID(); id != "" {
			exists[id] = true
		}
	}
	var dangling []string
	for _, t := range tasks {
		if tgt := t.Blocks(); tgt != "" && !exists[tgt] {
			dangling = append(dangling, t.ID())
		}
	}
	return dangling
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
