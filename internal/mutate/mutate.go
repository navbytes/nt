// Package mutate is the single ULID-keyed mutation engine shared by the CLI and
// (later) the TUI, implementing the write contract from SPEC §6.1–6.3. Every
// write to tasks.txt goes through Apply: acquire the lock, re-read the file
// from disk, run the caller's mutation against the fresh Doc while recording
// before-images, append the undo transaction, then atomically write. Nothing
// else writes tasks.txt, so a concurrent `nt add` can never be clobbered by a
// stale in-memory snapshot.
package mutate

import (
	"time"

	"github.com/navbytes/nt/internal/lock"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/undo"
)

// Engine owns the store and serializes all task-file writes.
type Engine struct {
	S *store.Store
}

// Open resolves the store and returns an engine.
func Open() (*Engine, error) {
	s, err := store.Open()
	if err != nil {
		return nil, err
	}
	return &Engine{S: s}, nil
}

// Today returns today's date in todo.txt form.
func Today() string { return time.Now().Format("2006-01-02") }

// Recorder accumulates the before/after images of touched tasks so a single
// undo transaction can reverse the whole mutation.
type Recorder struct {
	d       *task.Doc
	order   []string
	changes map[string]*undo.Change
	removed map[string]bool
}

func (r *Recorder) touch(id string) *undo.Change {
	if c, ok := r.changes[id]; ok {
		return c
	}
	c := &undo.Change{ID: id}
	r.changes[id] = c
	r.order = append(r.order, id)
	return c
}

// Before records a task's current line before the caller mutates it. Call it
// immediately prior to changing the task.
func (r *Recorder) Before(t *task.Task) {
	c := r.touch(t.ID())
	if c.Before == "" && !r.removed[t.ID()] {
		c.Before = t.Line()
	}
}

// Added records a newly appended task (no before-image; undo deletes it).
func (r *Recorder) Added(t *task.Task) { r.touch(t.ID()) }

// Removed records a task removed from the document, capturing its before-image.
func (r *Recorder) Removed(id, before string) {
	c := r.touch(id)
	c.Before = before
	r.removed[id] = true
}

// finalize computes after-images and returns the non-empty change set.
func (r *Recorder) finalize() []undo.Change {
	var out []undo.Change
	for _, id := range r.order {
		c := r.changes[id]
		if r.removed[id] {
			c.After = ""
		} else if t := r.d.FindByID(id); t != nil {
			c.After = t.Line()
		}
		if c.Before != c.After {
			out = append(out, *c)
		}
	}
	return out
}

// Apply runs fn under the lock against a freshly-read Doc, journals the
// resulting transaction, and atomically writes the file. fn mutates the Doc and
// uses rec to record what it touched.
func (e *Engine) Apply(op string, fn func(d *task.Doc, rec *Recorder) error) error {
	h, err := lock.Acquire(e.S.LockFile(), lock.DefaultTimeout)
	if err != nil {
		return err
	}
	defer h.Release()

	data, err := store.ReadFile(e.S.TasksFile())
	if err != nil {
		return err
	}
	d := task.Parse(data)
	rec := &Recorder{d: d, changes: map[string]*undo.Change{}, removed: map[string]bool{}}

	if err := fn(d, rec); err != nil {
		return err
	}

	changes := rec.finalize()
	if len(changes) == 0 {
		return nil // nothing changed; don't journal or rewrite
	}
	// Journal BEFORE the forward write (SPEC §6.3): a crash between the two
	// leaves a benign orphan entry, never a mutation without its inverse.
	if err := undo.Append(e.S, undo.Txn{Op: op, TS: time.Now().Format(time.RFC3339), Changes: changes}); err != nil {
		return err
	}
	return store.WriteAtomic(e.S.TasksFile(), d.Render(), 0o644)
}

// Read returns the current parsed document (no lock; read-only callers such as
// list/search tolerate a consistent-at-read-time snapshot).
func (e *Engine) Read() (*task.Doc, error) {
	data, err := store.ReadFile(e.S.TasksFile())
	if err != nil {
		return nil, err
	}
	return task.Parse(data), nil
}
