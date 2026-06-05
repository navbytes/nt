package mutate

import (
	"strings"

	"github.com/navbytes/nt/internal/lock"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// Archive moves completed tasks from tasks.txt to done.txt under the lock. It
// is an explicit housekeeping action and is intentionally not journaled — the
// cross-file move is the open question in SPEC §15, so `nt undo` does not
// revert it (the CLI says so). Returns the number of tasks moved.
func (e *Engine) Archive() (int, error) {
	h, err := lock.Acquire(e.S.LockFile(), lock.DefaultTimeout)
	if err != nil {
		return 0, err
	}
	defer h.Release()

	data, err := store.ReadFile(e.S.TasksFile())
	if err != nil {
		return 0, err
	}
	d := task.Parse(data)

	var moved []string
	kept := d.Nodes[:0]
	for _, n := range d.Nodes {
		if n.Task != nil && n.Task.Done {
			moved = append(moved, n.Task.Line())
			continue
		}
		kept = append(kept, n)
	}
	if len(moved) == 0 {
		return 0, nil
	}
	d.Nodes = kept

	// Append moved lines to done.txt.
	done, err := store.ReadFile(e.S.DoneFile())
	if err != nil {
		return 0, err
	}
	var b strings.Builder
	b.Write(done)
	if len(done) > 0 && !strings.HasSuffix(string(done), "\n") {
		b.WriteByte('\n')
	}
	for _, l := range moved {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if err := store.WriteAtomic(e.S.DoneFile(), []byte(b.String()), 0o644); err != nil {
		return 0, err
	}
	if err := store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644); err != nil {
		return 0, err
	}
	return len(moved), nil
}
