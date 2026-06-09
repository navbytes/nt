package tui

import (
	"testing"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/task"
)

// TestReloadSuppressesSelfWriteEcho verifies F4: a reload that finds the store
// unchanged (the fsnotify echo of our own write) skips the rebuild, while a
// reload after a genuine change still rebuilds.
func TestReloadSuppressesSelfWriteEcho(t *testing.T) {
	m := testModel(t) // performs an initial reload → lastSig set, view built

	// Sabotage the built view. If reload rebuilds, it will be repopulated; if it
	// correctly skips (store unchanged since the initial reload), it stays nil.
	m.flat = nil
	m.reload()
	if m.flat != nil {
		t.Fatal("reload rebuilt the view on an unchanged store — self-write echo not suppressed")
	}

	// A real store change must break the signature and trigger a rebuild.
	if err := m.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		nt := task.New("brand new task")
		d.Append(nt)
		rec.Added(nt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.reload()
	if len(m.flat) == 0 {
		t.Fatal("reload should rebuild after a genuine store change")
	}

	// And the echo of that change is suppressed in turn.
	m.flat = nil
	m.reload()
	if m.flat != nil {
		t.Fatal("second self-write echo not suppressed")
	}
}
