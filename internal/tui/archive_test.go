package tui

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// TestNoteArchiveTUI drives the soft retire through the key handlers: 'x' on the
// notes tab archives the selected note (it drops from the list), '.' reveals the
// retired set, and a second 'x' restores it.
func TestNoteArchiveTUI(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	_, _ = note.Create(m.eng.S, "Keeper", "durable decision", nil, "tui", "")
	_, _ = note.Create(m.eng.S, "Stale", "obsolete scratch", nil, "tui", "")
	m.reload()
	m.tab = tabNotes
	m.rebuild()
	if len(m.notesView) != 2 {
		t.Fatalf("precondition: 2 notes visible, got %d", len(m.notesView))
	}

	cursorTo := func(title string) {
		for i, n := range m.notesView {
			if n.Title == title {
				m.cursor = i
				return
			}
		}
		t.Fatalf("note %q not in the view", title)
	}
	onDiskArchived := func(title string) bool {
		notes, _ := note.List(m.eng.S)
		for _, n := range notes {
			if n.Title == title {
				return n.Archived
			}
		}
		t.Fatalf("note %q not found on disk", title)
		return false
	}

	// Archive "Stale" with 'x' → only Keeper remains in the (default) view.
	cursorTo("Stale")
	m = press(m, "x").(*Model)
	if len(m.notesView) != 1 || m.notesView[0].Title != "Keeper" {
		t.Fatalf("after archive only Keeper should show, got %d: %v", len(m.notesView), noteTitles(m.notesView))
	}
	if !onDiskArchived("Stale") {
		t.Error("Stale should be archived:true on disk")
	}

	// '.' reveals the retired set (both notes show again), with a marker so the
	// retired note reads as distinct from the working set.
	m = press(m, ".").(*Model)
	if !m.showArchived || len(m.notesView) != 2 {
		t.Fatalf("'.' should reveal archived → 2 notes (showArchived=%v len=%d)", m.showArchived, len(m.notesView))
	}
	if rendered := m.notesList(m.width, m.height); !strings.Contains(rendered, "·archived") {
		t.Errorf("a revealed archived note should carry the ·archived marker:\n%s", rendered)
	}

	// A second 'x' on Stale restores it.
	cursorTo("Stale")
	m = press(m, "x").(*Model)
	if onDiskArchived("Stale") {
		t.Error("a second 'x' should unarchive Stale")
	}
}

func noteTitles(ns []*note.Note) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Title
	}
	return out
}
