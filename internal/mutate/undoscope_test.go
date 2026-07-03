package mutate

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/task"
)

// TestUndoScopedOwnership: on a shared store, an agent must not silently revert
// another workstream's transaction — that's cross-agent data loss.
func TestUndoScopedOwnership(t *testing.T) {
	e := newEngine(t)
	t.Setenv("NT_WORKSTREAM", "agent-a")
	if err := e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("agent-a's task")
		d.Append(nt)
		rec.Added(nt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A different workstream is refused, with an actionable message.
	if _, did, err := e.UndoScoped("agent-b", false); did || err == nil {
		t.Fatalf("foreign undo: did=%v err=%v, want refusal", did, err)
	} else if !strings.Contains(err.Error(), "agent-a") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("refusal should name the owner and the escape hatch, got: %v", err)
	}
	// --force overrides; the txn is returned for display.
	if txn, did, err := e.UndoScoped("agent-b", true); err != nil || !did {
		t.Fatalf("forced undo: did=%v err=%v", did, err)
	} else if txn.WS != "agent-a" || len(txn.Changes) != 1 {
		t.Fatalf("returned txn should carry owner + changes, got ws=%q changes=%d", txn.WS, len(txn.Changes))
	}
	// The swapped redo entry keeps the original owner: agent-b can't redo either.
	if _, did, err := e.Redo("agent-b", false); did || err == nil {
		t.Fatalf("foreign redo: did=%v err=%v, want refusal", did, err)
	}
	// The owner can redo.
	if _, did, err := e.Redo("agent-a", false); err != nil || !did {
		t.Fatalf("owner redo: did=%v err=%v", did, err)
	}
}

// TestUndoScopedOwnerAndUnscoped: the owner undoes its own txn without force,
// an unscoped (human) caller undoes anything, and a scoped agent is refused on
// an unscoped writer's txn (pre-workstream journals, human CLI writes).
func TestUndoScopedOwnerAndUnscoped(t *testing.T) {
	e := newEngine(t)
	t.Setenv("NT_WORKSTREAM", "agent-a")
	add := func(title string) {
		if err := e.Apply("add", func(d *task.Doc, rec *Recorder) error {
			nt := task.New(title)
			d.Append(nt)
			rec.Added(nt)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("first")
	if _, did, err := e.UndoScoped("agent-a", false); err != nil || !did {
		t.Fatalf("owner undo: did=%v err=%v", did, err)
	}

	t.Setenv("NT_WORKSTREAM", "")
	add("human's task")
	// Scoped agent may not undo the human's change without force…
	if _, did, err := e.UndoScoped("agent-a", false); did || err == nil {
		t.Fatalf("agent undoing unscoped txn: did=%v err=%v, want refusal", did, err)
	}
	// …but an unscoped caller (ws == "") undoes anything.
	if _, did, err := e.UndoScoped("", false); err != nil || !did {
		t.Fatalf("unscoped undo: did=%v err=%v", did, err)
	}
}

// TestPlainUndoRefusesPendingRedo: after an undo, another `undo` must not
// silently re-apply the change (the old toggle behavior) — it should point at
// redo instead.
func TestPlainUndoRefusesPendingRedo(t *testing.T) {
	e := newEngine(t)
	if err := e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("toggle guard")
		d.Append(nt)
		rec.Added(nt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, did, err := e.Undo(); err != nil || !did {
		t.Fatalf("undo: did=%v err=%v", did, err)
	}
	if _, did, err := e.Undo(); did || err == nil || !strings.Contains(err.Error(), "redo") {
		t.Fatalf("second undo should refuse and point at redo, got did=%v err=%v", did, err)
	}
	// Redo without a pending redo entry reports nothing-to-redo (did=false, no error).
	if _, did, err := e.Redo("", true); err != nil || !did {
		t.Fatalf("redo after undo: did=%v err=%v", did, err)
	}
	if _, did, err := e.Redo("", true); did || err != nil {
		t.Fatalf("redo with forward top: did=%v err=%v, want no-op", did, err)
	}
}
