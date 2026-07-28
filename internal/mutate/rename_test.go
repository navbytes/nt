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

// TestTrashNoteNeverClobbers is the regression guard for silent data loss:
// TrashNote used to os.Rename straight onto .trash/<flattened rel>, and
// os.Rename overwrites. Deleting the same rel twice therefore destroyed the
// first copy — and .trash/ is the only recovery path, since TrashNote is not a
// journaled undo transaction.
func TestTrashNoteNeverClobbers(t *testing.T) {
	e := newEngine(t)
	trash := filepath.Join(e.S.Dir, ".trash")

	write := func(rel, body string) *note.Note {
		t.Helper()
		p := filepath.Join(e.S.NotesDir(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return &note.Note{Path: p, Rel: rel}
	}

	// Same rel, trashed twice: both copies must survive.
	if err := e.TrashNote(write("ref/retry-policy.md", "VERSION ONE")); err != nil {
		t.Fatal(err)
	}
	if err := e.TrashNote(write("ref/retry-policy.md", "VERSION TWO")); err != nil {
		t.Fatal(err)
	}
	// Distinct notes that FLATTEN to the same trash name must also both survive.
	if err := e.TrashNote(write("ref_retry-policy.md", "FLATTENED")); err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	entries, err := os.ReadDir(trash)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range entries {
		b, err := os.ReadFile(filepath.Join(trash, de.Name()))
		if err != nil {
			t.Fatal(err)
		}
		found[string(b)] = true
	}
	for _, want := range []string{"VERSION ONE", "VERSION TWO", "FLATTENED"} {
		if !found[want] {
			t.Errorf("trash lost %q — .trash/ holds %d file(s): %v", want, len(entries), found)
		}
	}
}

// TestRenameNoteSlugsDestination: `nt mv` used to write the destination
// verbatim, so a human-readable name produced the one file in the store with
// literal spaces in it — unlike `nt note`, which always slugs.
func TestRenameNoteSlugsDestination(t *testing.T) {
	e := newEngine(t)
	p := filepath.Join(e.S.NotesDir(), "start.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# Start\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := &note.Note{Path: p, Rel: "start.md", Title: "Start"}

	newRel, _, err := e.RenameNote(src, []*note.Note{src}, "ref/My Long Name")
	if err != nil {
		t.Fatal(err)
	}
	if newRel != "ref/my-long-name.md" {
		t.Errorf("destination should be slugged, got %q", newRel)
	}
	if _, err := os.Stat(filepath.Join(e.S.NotesDir(), "ref", "my-long-name.md")); err != nil {
		t.Errorf("slugged file missing: %v", err)
	}
}
