package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

func press(model tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var t tea.KeyMsg
		switch k {
		case "ctrl+d":
			t = tea.KeyMsg{Type: tea.KeyCtrlD}
		default:
			t = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		model, _ = model.Update(t)
	}
	return model
}

// TestXTogglesDone: the single-key `x` completes the selected task.
func TestXTogglesDone(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	id := m.selectedTask().ID()
	mm := press(m, "x").(*Model)
	if tk := mm.eng2find(t, id); !tk.Done {
		t.Fatal("x should complete the selected task")
	}
}

// TestPreserveSelectionAcrossRegroup: cursor follows the same item after `v`.
func TestPreserveSelectionAcrossRegroup(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	m = press(m, "j").(*Model) // move off the first row
	before := m.selectedTask().ID()
	m = press(m, "v").(*Model) // regroup date→project
	if after := m.selectedTask().ID(); after != before {
		t.Fatalf("selection should follow the item across regroup: %s -> %s", before, after)
	}
}

// TestHalfPageScroll: Ctrl+d advances the cursor by a half page.
func TestHalfPageScroll(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	// add enough tasks to scroll
	for i := 0; i < 30; i++ {
		_ = m.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
			nt := task.New("filler task")
			d.Append(nt)
			rec.Added(nt)
			return nil
		})
	}
	m.reload()
	m.cursor = 0
	mm := press(m, "ctrl+d").(*Model)
	if mm.cursor != mm.halfPage() {
		t.Fatalf("ctrl+d should move cursor by halfPage (%d), got %d", mm.halfPage(), mm.cursor)
	}
}

// TestNotesFilterSearchesBody: the notes filter matches body text.
func TestNotesFilterSearchesBody(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	_, _ = note.Create(m.eng.S, "Alpha", "the quick brown fox", nil, "tui", "")
	_, _ = note.Create(m.eng.S, "Beta", "lazy dog sleeps", nil, "tui", "")
	m.reload()
	m.tab = tabNotes
	m.filter = "brown"
	m.rebuild()
	if len(m.notesView) != 1 || m.notesView[0].Title != "Alpha" {
		t.Fatalf("body filter should match 1 note (Alpha), got %d", len(m.notesView))
	}
}

// TestRemoveTag: `T` flow removes a tag from the selected task.
func TestRemoveTag(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	id := m.selectedTask().ID()
	m.addTag("urgent")
	if got := m.eng2find(t, id); !contains(got.Tags(), "urgent") {
		t.Fatal("addTag should have added @urgent")
	}
	m.removeTag("urgent")
	if got := m.eng2find(t, id); contains(got.Tags(), "urgent") {
		t.Fatal("removeTag should have removed @urgent")
	}
}

// eng2find reloads and returns the task with the given id (test helper).
func (m *Model) eng2find(t *testing.T, id string) *task.Task {
	d, err := m.eng.Read()
	if err != nil {
		t.Fatal(err)
	}
	tk := d.FindByID(id)
	if tk == nil {
		t.Fatalf("task %s not found", id)
	}
	return tk
}

// TestUndoRedoKeys: `u` undoes and `U` redoes, split by direction — a second
// `u` does not redo (and a second `U` does not undo).
func TestUndoRedoKeys(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	id := m.selectedTask().ID()

	m = press(m, "x").(*Model) // complete it
	if !m.eng2find(t, id).Done {
		t.Fatal("x should complete the task")
	}
	m = press(m, "u").(*Model) // undo
	if m.eng2find(t, id).Done {
		t.Fatal("u should undo the completion")
	}
	m = press(m, "u").(*Model) // a second undo must NOT redo
	if m.eng2find(t, id).Done {
		t.Fatal("a second u must not redo — use U")
	}
	m = press(m, "U").(*Model) // redo
	if !m.eng2find(t, id).Done {
		t.Fatal("U should redo the completion")
	}
	m = press(m, "U").(*Model) // a second redo must NOT undo
	if !m.eng2find(t, id).Done {
		t.Fatal("a second U must not undo")
	}
}

// TestFollowLinkPicker: L on an item with several links opens the follow picker
// (instead of silently following the first); with one link it follows directly.
func TestFollowLinkPicker(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24

	var multi, single string
	_ = m.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		a := task.New("see [[Alpha]] and [[Beta]]")
		b := task.New("only [[Gamma]]")
		d.Append(a)
		d.Append(b)
		rec.Added(a)
		rec.Added(b)
		multi, single = a.ID(), b.ID()
		return nil
	})
	m.reload()

	// Multi-link → picker opens listing both links.
	m.selectByID(multi)
	m = press(m, "L").(*Model)
	if !m.followMode || len(m.followTargets) != 2 {
		t.Fatalf("L on a 2-link task should open a 2-item picker, got followMode=%v n=%d", m.followMode, len(m.followTargets))
	}
	if m.followTargets[0].value != "Alpha" || m.followTargets[1].value != "Beta" {
		t.Errorf("picker targets wrong: %+v", m.followTargets)
	}
	m = press(m, "esc").(*Model) // cancel
	if m.followMode {
		t.Fatal("esc should cancel the picker")
	}

	// Single-link → follows directly, no picker.
	m.selectByID(single)
	m = press(m, "L").(*Model)
	if m.followMode {
		t.Fatal("L on a single-link task should not open a picker")
	}
}

// TestAddLinkToNoteFromNotesTab: `l` on the notes tab appends a [[wikilink]] to
// the selected note's body (was tasks-only before — U7).
func TestAddLinkToNoteFromNotesTab(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	n, err := note.Create(m.eng.S, "Target Note", "original body", nil, "tui", "")
	if err != nil {
		t.Fatal(err)
	}
	m.reload()
	m.tab = tabNotes
	m.cursor = 0
	if m.selectedNote() == nil {
		t.Fatal("expected a selected note on the notes tab")
	}

	m = press(m, "l").(*Model)     // open the link prompt
	m = press(m, "Other").(*Model) // type the target (one multi-rune msg)
	press(m, "enter")              // commit (note is written to disk; m unused after)

	got, _ := note.Load(n.Path)
	if !strings.Contains(got.Body, "[[Other]]") {
		t.Fatalf("note body should contain the added wikilink:\n%s", got.Body)
	}
	if !strings.Contains(got.Body, "original body") {
		t.Errorf("existing body should be preserved:\n%s", got.Body)
	}
}

// TestVimCountsAndTabKeys: digit prefixes repeat motions (3j/2k/5G), and tabs
// moved to [ / ] (U6).
func TestVimCountsAndTabKeys(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	for i := 0; i < 10; i++ {
		_ = m.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
			nt := task.New("filler task")
			d.Append(nt)
			rec.Added(nt)
			return nil
		})
	}
	m.reload()
	m.cursor = 0

	if mm := press(m, "3", "j").(*Model); mm.cursor != 3 {
		t.Fatalf("3j should land on row 3, got %d", mm.cursor)
	}
	m.cursor = 3
	if mm := press(m, "2", "k").(*Model); mm.cursor != 1 {
		t.Fatalf("2k from row 3 should be row 1, got %d", mm.cursor)
	}
	if mm := press(m, "5", "G").(*Model); mm.cursor != 4 {
		t.Fatalf("5G should jump to row 5 (index 4), got %d", mm.cursor)
	}

	// Tab keys: ] next, [ prev.
	m.tab = tabTasks
	if mm := press(m, "]").(*Model); mm.tab != tabNotes {
		t.Fatalf("] should advance to the notes tab, got %v", mm.tab)
	}
	m.tab = tabTasks
	if mm := press(m, "[").(*Model); mm.tab != tabLogbook {
		t.Fatalf("[ should wrap to the logbook tab, got %v", mm.tab)
	}
}
