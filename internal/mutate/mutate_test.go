package mutate

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

func newEngine(t *testing.T) *Engine {
	t.Setenv("NT_DIR", t.TempDir())
	e, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func tasks(t *testing.T, e *Engine) []*task.Task {
	d, err := e.Read()
	if err != nil {
		t.Fatal(err)
	}
	return d.Tasks()
}

// TestAddThenUndoRemoves verifies that undoing an add deletes the task.
func TestAddThenUndoRemoves(t *testing.T) {
	e := newEngine(t)
	var id string
	if err := e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("hello world")
		d.Append(nt)
		rec.Added(nt)
		id = nt.ID()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := tasks(t, e); len(got) != 1 || got[0].ID() != id {
		t.Fatalf("want 1 task after add, got %d", len(got))
	}
	op, did, err := e.Undo()
	if err != nil || !did || op != "add" {
		t.Fatalf("undo add: op=%q did=%v err=%v", op, did, err)
	}
	if got := tasks(t, e); len(got) != 0 {
		t.Fatalf("want 0 tasks after undo, got %d", len(got))
	}
}

// TestDoneUndoRedo verifies done → undo (reopen) → undo (redo, re-complete).
func TestDoneUndoRedo(t *testing.T) {
	e := newEngine(t)
	_ = e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("ship it")
		d.Append(nt)
		rec.Added(nt)
		return nil
	})
	if err := e.Apply("done", func(d *task.Doc, rec *Recorder) error {
		tk := d.Tasks()[0]
		rec.Before(tk)
		tk.SetDone(true, "2026-06-06")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !tasks(t, e)[0].Done {
		t.Fatal("task should be done")
	}
	// Undo reopens.
	if _, _, err := e.Undo(); err != nil {
		t.Fatal(err)
	}
	if tasks(t, e)[0].Done {
		t.Fatal("undo should have reopened the task")
	}
	// Undo again redoes (re-completes).
	if _, did, _ := e.Undo(); !did {
		t.Fatal("expected a redo transaction to exist")
	}
	if !tasks(t, e)[0].Done {
		t.Fatal("redo should have re-completed the task")
	}
}

// TestCompleteRecurrenceUndo verifies completing a recurring task spawns the
// next occurrence, and undo reverses both in one transaction (SPEC §6.3, §9).
func TestCompleteRecurrenceUndo(t *testing.T) {
	e := newEngine(t)
	_ = e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("weekly review")
		nt.SetKey("due", "2026-06-06")
		nt.SetKey("rec", "weekly")
		d.Append(nt)
		rec.Added(nt)
		return nil
	})
	id := tasks(t, e)[0].ID()

	_ = e.Apply("done", func(d *task.Doc, rec *Recorder) error {
		Complete(d, rec, d.FindByID(id), "2026-06-06")
		return nil
	})
	got := tasks(t, e)
	if len(got) != 2 {
		t.Fatalf("completing a recurring task should leave 2 tasks, got %d", len(got))
	}
	var spawned *task.Task
	for _, tk := range got {
		if tk.ID() != id {
			spawned = tk
		}
	}
	if spawned == nil || spawned.Due() != "2026-06-13" || spawned.Done {
		t.Fatalf("spawned occurrence wrong: %+v", spawned)
	}

	if _, _, err := e.Undo(); err != nil {
		t.Fatal(err)
	}
	got = tasks(t, e)
	if len(got) != 1 {
		t.Fatalf("undo should remove the spawn, leaving 1 task, got %d", len(got))
	}
	if got[0].Done {
		t.Fatal("undo should reopen the original")
	}
}

// addAndComplete adds a task and completes it, leaving a "done" transaction on
// top of the undo journal whose post-image is the completed line. Returns the id.
func addAndComplete(t *testing.T, e *Engine) string {
	t.Helper()
	var id string
	if err := e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("fragile task")
		d.Append(nt)
		rec.Added(nt)
		id = nt.ID()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Apply("done", func(d *task.Doc, rec *Recorder) error {
		tk := d.FindByID(id)
		rec.Before(tk)
		tk.SetDone(true, "2026-06-06")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestUndoRefusesAndDoesNotResurrect: if a touched task is removed underneath us
// (an unjournaled archive/sync, here simulated by an external write) before we
// undo, undo must refuse rather than re-append (resurrect) the task — SPEC §6.1
// "never resurrects a removed task", §6.3 "if the world moved underneath, it
// refuses". This is the regression guard for the resurrection bug.
func TestUndoRefusesAndDoesNotResurrect(t *testing.T) {
	e := newEngine(t)
	_ = addAndComplete(t, e)

	// Another writer removes the task entirely, without a journal entry.
	if err := store.WriteAtomic(e.S.TasksFile(), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	op, did, err := e.Undo()
	if err == nil || did {
		t.Fatalf("undo should refuse when the task was removed underneath (op=%q did=%v err=%v)", op, did, err)
	}
	if got := tasks(t, e); len(got) != 0 {
		t.Fatalf("undo resurrected a removed task: got %d tasks, want 0", len(got))
	}
}

// TestUndoRefusesWhenChangedUnderneath: if a touched task was modified by another
// writer after the recorded transaction, undo refuses and the other writer's
// change survives intact (not clobbered by the stale before-image).
func TestUndoRefusesWhenChangedUnderneath(t *testing.T) {
	e := newEngine(t)
	id := addAndComplete(t, e)

	// Another writer edits the same task (adds a token), bypassing the journal.
	data, _ := store.ReadFile(e.S.TasksFile())
	d := task.Parse(data)
	d.FindByID(id).SetKey("note", "edited")
	if err := store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644); err != nil {
		t.Fatal(err)
	}

	if op, did, err := e.Undo(); err == nil || did {
		t.Fatalf("undo should refuse when the task changed underneath (op=%q did=%v err=%v)", op, did, err)
	}
	out, _ := store.ReadFile(e.S.TasksFile())
	if !strings.Contains(string(out), "note:edited") {
		t.Fatalf("refused undo clobbered the concurrent edit:\n%s", out)
	}
}

// TestConcurrentAddsNoLostUpdate runs many adds concurrently through the engine
// and confirms none are lost — the lock + re-read-before-write contract holds.
func TestConcurrentAddsNoLostUpdate(t *testing.T) {
	e := newEngine(t)
	const n = 25
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			done <- e.Apply("add", func(d *task.Doc, rec *Recorder) error {
				nt := task.New("task")
				d.Append(nt)
				rec.Added(nt)
				return nil
			})
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if got := tasks(t, e); len(got) != n {
		t.Fatalf("want %d tasks, got %d (lost update)", n, len(got))
	}
}

func TestPeekUndoTracksDirection(t *testing.T) {
	e := newEngine(t)

	// Empty journal → nothing to peek.
	if _, _, ok := e.PeekUndo(); ok {
		t.Fatal("empty journal should peek not-ok")
	}

	if err := e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		nt := task.New("hello")
		d.Append(nt)
		rec.Added(nt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Fresh forward op: undoable, not a redo.
	if label, isRedo, ok := e.PeekUndo(); !ok || isRedo || label != "add" {
		t.Fatalf("after add: label=%q isRedo=%v ok=%v, want add/false/true", label, isRedo, ok)
	}

	if _, did, err := e.Undo(); err != nil || !did {
		t.Fatalf("undo: did=%v err=%v", did, err)
	}
	// After undo: a redo is pending (label still 'add', stripped of redo:).
	if label, isRedo, ok := e.PeekUndo(); !ok || !isRedo || label != "add" {
		t.Fatalf("after undo: label=%q isRedo=%v ok=%v, want add/true/true", label, isRedo, ok)
	}

	if _, did, err := e.Undo(); err != nil || !did { // toggles: redoes the add
		t.Fatalf("redo: did=%v err=%v", did, err)
	}
	// After redo: forward again (direction tracking, not stuck on the prefix).
	if label, isRedo, ok := e.PeekUndo(); !ok || isRedo || label != "add" {
		t.Fatalf("after redo: label=%q isRedo=%v ok=%v, want add/false/true", label, isRedo, ok)
	}
}
