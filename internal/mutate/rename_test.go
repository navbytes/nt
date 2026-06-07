package mutate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

func findNote(t *testing.T, e *Engine, name string) *note.Note {
	t.Helper()
	all, _ := note.List(e.S)
	for _, x := range all {
		if base(x.Rel) == name {
			return x
		}
	}
	t.Fatalf("note %q not found", name)
	return nil
}

func TestRenameNoteRewritesLinks(t *testing.T) {
	e := newEngine(t)
	_, _ = note.Create(e.S, "Old Note", "the note body", nil, "cli", "")
	ref, _ := note.Create(e.S, "Ref", "see [[old-note]] and [[old-note#sec|plan]]", nil, "cli", "")
	_ = e.Apply("add", func(d *task.Doc, rec *Recorder) error {
		tk := task.New("do [[old-note]] thing")
		d.Append(tk)
		rec.Added(tk)
		return nil
	})

	all, _ := note.List(e.S)
	newRel, updated, err := e.RenameNote(findNote(t, e, "old-note"), all, "new-note")
	if err != nil {
		t.Fatal(err)
	}
	if newRel != "new-note.md" || updated < 2 {
		t.Fatalf("rename result: rel=%q updated=%d", newRel, updated)
	}
	if _, err := os.Stat(filepath.Join(e.S.NotesDir(), "new-note.md")); err != nil {
		t.Fatal("file was not renamed")
	}
	if _, err := os.Stat(filepath.Join(e.S.NotesDir(), "old-note.md")); err == nil {
		t.Fatal("old file should be gone")
	}
	d, _ := e.Read()
	if d.Tasks()[0].Text != "do [[new-note]] thing" {
		t.Fatalf("task link not rewritten: %q", d.Tasks()[0].Text)
	}
	body, _ := note.Load(ref.Path)
	if !strings.Contains(body.Body, "[[new-note]]") || !strings.Contains(body.Body, "[[new-note#sec|plan]]") {
		t.Fatalf("note links/alias/fragment not preserved: %q", body.Body)
	}
}

func TestRenameNoteCollisionRefused(t *testing.T) {
	e := newEngine(t)
	_, _ = note.Create(e.S, "Alpha", "", nil, "cli", "")
	_, _ = note.Create(e.S, "Beta", "", nil, "cli", "")
	all, _ := note.List(e.S)
	if _, _, err := e.RenameNote(findNote(t, e, "alpha"), all, "beta"); err == nil {
		t.Fatal("alpha→beta should be refused (basename collision)")
	}
}

func TestPureMoveNoRewrite(t *testing.T) {
	e := newEngine(t)
	_, _ = note.Create(e.S, "Spec", "no links here", nil, "cli", "")
	all, _ := note.List(e.S)
	newRel, updated, err := e.RenameNote(findNote(t, e, "spec"), all, "archive/spec")
	if err != nil {
		t.Fatal(err)
	}
	if newRel != "archive/spec.md" || updated != 0 {
		t.Fatalf("pure move should rewrite nothing: rel=%q updated=%d", newRel, updated)
	}
	if _, err := os.Stat(filepath.Join(e.S.NotesDir(), "archive", "spec.md")); err != nil {
		t.Fatal("file was not moved into the subfolder")
	}
}
