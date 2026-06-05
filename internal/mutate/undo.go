package mutate

import (
	"time"

	"github.com/navbytes/nt/internal/lock"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/undo"
)

// Undo reverts the most recent transaction. It pops the journal, applies each
// change's inverse (restoring before-images by ULID), writes the file, and
// appends a swapped transaction so a subsequent `nt undo` redoes the change
// (SPEC §6.3). The returned op label names what was undone; did is false when
// there is nothing to undo.
func (e *Engine) Undo() (op string, did bool, err error) {
	h, err := lock.Acquire(e.S.LockFile(), lock.DefaultTimeout)
	if err != nil {
		return "", false, err
	}
	defer h.Release()

	data, err := store.ReadFile(e.S.TasksFile())
	if err != nil {
		return "", false, err
	}
	d := task.Parse(data)

	txn, ok, err := undo.Pop(e.S)
	if err != nil || !ok {
		return "", false, err
	}
	for _, c := range txn.Changes {
		applyInverse(d, c)
	}
	if err := store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644); err != nil {
		return "", false, err
	}

	// Record the redo transaction (Before/After swapped).
	redo := undo.Txn{Op: "redo:" + txn.Op, TS: time.Now().Format(time.RFC3339)}
	for _, c := range txn.Changes {
		redo.Changes = append(redo.Changes, undo.Change{ID: c.ID, Before: c.After, After: c.Before})
	}
	if err := undo.Append(e.S, redo); err != nil {
		return "", false, err
	}
	return txn.Op, true, nil
}

// applyInverse restores a change's Before image, keyed by ULID. An empty Before
// means the task was added by the forward op, so the inverse removes it.
func applyInverse(d *task.Doc, c undo.Change) {
	if c.Before == "" {
		d.Remove(c.ID)
		return
	}
	bt, ok := task.ParseLine(c.Before)
	if !ok {
		return
	}
	if !d.ReplaceByID(c.ID, bt) {
		d.Append(bt)
	}
}
