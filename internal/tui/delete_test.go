package tui

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// TestNoteDeleteWithBacklinksTUI: pressing 'X' on a note with inbound links
// surfaces a multi-choice confirm; 'u' strips the links then trashes the note.
func TestNoteDeleteWithBacklinksTUI(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	_, _ = note.Create(m.eng.S, "Target", "the target", nil, "tui", "ref")
	_, _ = note.Create(m.eng.S, "Referrer", "see [[Target]] here", nil, "tui", "ref")
	m.reload()
	m.tab = tabNotes
	m.rebuild()

	cursorTo := func(title string) {
		for i, n := range m.notesView {
			if n.Title == title {
				m.cursor = i
				return
			}
		}
		t.Fatalf("note %q not in view", title)
	}
	onDisk := func(title string) bool {
		notes, _ := note.List(m.eng.S)
		for _, n := range notes {
			if n.Title == title {
				return true
			}
		}
		return false
	}

	cursorTo("Target")
	m = press(m, "X").(*Model)
	if m.confirm == nil || m.confirm.choices == nil {
		t.Fatal("X on a linked note should arm a multi-choice confirm")
	}

	// 'u' → unlink & delete.
	m = press(m, "u").(*Model)
	if onDisk("Target") {
		t.Fatal("Target should be trashed after unlink & delete")
	}
	notes, _ := note.List(m.eng.S)
	for _, n := range notes {
		if n.Title == "Referrer" && strings.Contains(n.Body, "[[Target]]") {
			t.Fatalf("inbound link should have been stripped: %q", n.Body)
		}
	}
}

// TestNoteDeleteNoBacklinksTUI: a note nothing links to gets a plain y/n confirm,
// and 'y' trashes it.
func TestNoteDeleteNoBacklinksTUI(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	_, _ = note.Create(m.eng.S, "Lonely", "no links here", nil, "tui", "ref")
	m.reload()
	m.tab = tabNotes
	m.rebuild()
	m.cursor = 0

	m = press(m, "X").(*Model)
	if m.confirm == nil || m.confirm.choices != nil {
		t.Fatal("X on an unlinked note should arm a binary confirm")
	}
	m = press(m, "y").(*Model)
	notes, _ := note.List(m.eng.S)
	if len(notes) != 0 {
		t.Fatalf("note should be trashed, still present: %v", noteTitles(m.notesView))
	}
}
