package mutate

import (
	"testing"

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
