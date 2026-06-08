package mutate

import (
	"fmt"
	"time"

	"github.com/navbytes/nt/internal/lock"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/undo"
)

// Undo reverts the most recent transaction (SPEC §6.3). Under the lock it:
//  1. validates that current state still matches the transaction's recorded
//     post-image by ULID — if the world moved underneath (another writer
//     changed/removed a touched task), it refuses rather than corrupting state;
//  2. applies each change's inverse (restoring before-images by ULID) WITHOUT
//     resurrecting a task another writer removed;
//  3. durably writes the reverted tasks file FIRST, then removes the journal
//     entry and records a swapped redo transaction — so a tasks-write failure
//     can't lose the inverse (it stays in the journal, retryable).
//
// The returned op label names what was undone; did is false when there is
// nothing to undo.
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

	// Peek (don't remove) so a later failure leaves the journal intact.
	txn, ok, err := undo.Peek(e.S)
	if err != nil || !ok {
		return "", false, err
	}

	// Validate the post-image: every touched task must still be exactly as the
	// forward op left it. Otherwise a concurrent writer moved underneath us.
	if err := validatePostImage(d, txn); err != nil {
		return "", false, err
	}

	for _, c := range txn.Changes {
		if err := applyInverse(d, c); err != nil {
			return "", false, err
		}
	}

	// Tasks first: if this fails, the journal still holds the txn (retryable).
	if err := store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644); err != nil {
		return "", false, err
	}

	// Journal second: drop the undone txn and push its swapped redo, atomically.
	redo := undo.Txn{Op: "redo:" + txn.Op, TS: time.Now().Format(time.RFC3339)}
	for _, c := range txn.Changes {
		redo.Changes = append(redo.Changes, undo.Change{ID: c.ID, Before: c.After, After: c.Before})
	}
	if err := undo.ReplaceLast(e.S, redo); err != nil {
		return "", false, err
	}
	return txn.Op, true, nil
}

// validatePostImage checks that the live document still matches what the
// transaction recorded as its result (the After image), keyed by ULID. A change
// that added or modified a task (After != "") requires that task to be present
// and byte-identical; a change that removed a task (After == "") requires it to
// still be absent. Any mismatch means another writer moved underneath — undo
// refuses rather than clobber that write (SPEC §6.1/§6.3).
func validatePostImage(d *task.Doc, txn undo.Txn) error {
	for _, c := range txn.Changes {
		cur := d.FindByID(c.ID)
		if c.After == "" {
			if cur != nil {
				return fmt.Errorf("cannot undo %q: task %s was re-created since", txn.Op, short(c.ID))
			}
			continue
		}
		if cur == nil {
			return fmt.Errorf("cannot undo %q: task %s was removed since", txn.Op, short(c.ID))
		}
		if cur.Line() != c.After {
			return fmt.Errorf("cannot undo %q: task %s changed since", txn.Op, short(c.ID))
		}
	}
	return nil
}

// applyInverse restores a change's before-image, keyed by ULID. Before == ""
// undoes an add (remove the task). After == "" undoes a removal (re-add the
// line). Otherwise it undoes a modification (replace in place) — and never
// Appends a missing id, which would resurrect a task removed by another writer.
// validatePostImage runs first, so these operations are guaranteed to apply.
func applyInverse(d *task.Doc, c undo.Change) error {
	if c.Before == "" {
		d.Remove(c.ID)
		return nil
	}
	bt, ok := task.ParseLine(c.Before)
	if !ok {
		return fmt.Errorf("undo: corrupt before-image for %s", short(c.ID))
	}
	if c.After == "" {
		d.Append(bt) // undoing a removal: the id was validated absent
		return nil
	}
	if !d.ReplaceByID(c.ID, bt) {
		return fmt.Errorf("undo: task %s vanished mid-undo", short(c.ID))
	}
	return nil
}
