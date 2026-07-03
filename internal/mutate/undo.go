package mutate

import (
	"fmt"
	"strings"
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
	txn, did, err := e.UndoScoped("", true)
	return txn.Op, did, err
}

// UndoScoped is Undo with workstream ownership enforced, for shared stores
// where several agents write concurrently: when ws is non-empty and the pending
// transaction was written by a DIFFERENT workstream (or by an unscoped writer),
// it refuses — unless force — so one agent can't silently revert another's
// work. The check runs under the same lock as the revert, so the answer can't
// go stale between look and act. The reverted transaction is returned so the
// caller can show exactly what changed.
func (e *Engine) UndoScoped(ws string, force bool) (undo.Txn, bool, error) {
	return e.applyReversal(ws, force, false)
}

// Redo re-applies the most recently undone transaction (the swapped "redo:"
// entry Undo leaves on the journal). did is false when the pending entry is not
// a redo — i.e. there is nothing to redo. Ownership is enforced like UndoScoped.
func (e *Engine) Redo(ws string, force bool) (undo.Txn, bool, error) {
	return e.applyReversal(ws, force, true)
}

// applyReversal implements Undo/Redo: the journal's top entry toggles between
// the forward image and its inverse, so both operations apply the same way —
// redo just insists the pending entry IS a redo entry, and undo that it isn't.
func (e *Engine) applyReversal(ws string, force, wantRedo bool) (undo.Txn, bool, error) {
	h, err := lock.Acquire(e.S.LockFile(), lock.DefaultTimeout)
	if err != nil {
		return undo.Txn{}, false, err
	}
	defer h.Release()

	data, err := store.ReadFile(e.S.TasksFile())
	if err != nil {
		return undo.Txn{}, false, err
	}
	d := task.Parse(data)

	// Peek (don't remove) so a later failure leaves the journal intact.
	txn, ok, err := undo.Peek(e.S)
	if err != nil || !ok {
		return undo.Txn{}, false, err
	}
	if isRedoEntry(txn.Op) != wantRedo {
		if wantRedo {
			return undo.Txn{}, false, nil // top entry is a forward op: nothing to redo
		}
		// Top entry is a redo entry; plain undo would re-apply (redo) it, which is
		// not what "undo" means. Redo entries only sit on top immediately after an
		// undo, so tell the caller which verb they want.
		return undo.Txn{}, false, fmt.Errorf("the last operation was an undo — `nt redo` re-applies it; there is nothing further to undo")
	}
	if !force && ws != "" && txn.WS != ws {
		owner := "an unscoped writer (no workstream)"
		if txn.WS != "" {
			owner = fmt.Sprintf("workstream %q", txn.WS)
		}
		verb := "undo"
		if wantRedo {
			verb = "redo"
		}
		return undo.Txn{}, false, fmt.Errorf("the last change (%s, %s) was made by %s, not yours (%q) — on a shared store reverting another agent's work loses data; rerun `nt %s --force` if you really mean it", displayOp(txn.Op), txn.TS, owner, ws, verb)
	}

	// Validate the post-image: every touched task must still be exactly as the
	// forward op left it. Otherwise a concurrent writer moved underneath us.
	if err := validatePostImage(d, txn); err != nil {
		return undo.Txn{}, false, err
	}

	for _, c := range txn.Changes {
		if err := applyInverse(d, c); err != nil {
			return undo.Txn{}, false, err
		}
	}

	// Tasks first: if this fails, the journal still holds the txn (retryable).
	if err := store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644); err != nil {
		return undo.Txn{}, false, err
	}

	// Journal second: drop the undone txn and push its swapped redo, atomically.
	// The swapped entry keeps the original writer's workstream so ownership
	// survives an undo/redo round-trip.
	redo := undo.Txn{Op: "redo:" + txn.Op, TS: time.Now().Format(time.RFC3339), WS: txn.WS}
	for _, c := range txn.Changes {
		redo.Changes = append(redo.Changes, undo.Change{ID: c.ID, Before: c.After, After: c.Before})
	}
	if err := undo.ReplaceLast(e.S, redo); err != nil {
		return undo.Txn{}, false, err
	}
	return txn, true, nil
}

// isRedoEntry reports whether an op label marks a pending redo: undo/redo swap
// the top entry back and forth, each pass prefixing "redo:", so an odd number
// of prefixes means the entry re-applies a previously undone op.
func isRedoEntry(op string) bool {
	n := 0
	for strings.HasPrefix(op, "redo:") {
		op = op[len("redo:"):]
		n++
	}
	return n%2 == 1
}

// displayOp strips the internal redo: prefixes off an op label for messages.
func displayOp(op string) string {
	for strings.HasPrefix(op, "redo:") {
		op = op[len("redo:"):]
	}
	return op
}

// PeekUndo reports what the next reversal targets, without changing anything: the
// human label (with the internal "redo:" prefixes stripped) and whether the
// pending journal entry is a redo (an odd number of "redo:" prefixes) rather than
// a fresh forward op. ok is false when the journal is empty. It is a display-only
// read of the atomically-written journal, so it runs lock-free.
func (e *Engine) PeekUndo() (label string, isRedo bool, ok bool) {
	txn, found, err := undo.Peek(e.S)
	if err != nil || !found {
		return "", false, false
	}
	op, n := txn.Op, 0
	for strings.HasPrefix(op, "redo:") {
		op = op[len("redo:"):]
		n++
	}
	return op, n%2 == 1, true
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
