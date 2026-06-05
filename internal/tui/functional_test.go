package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navbytes/nt/internal/note"
)

// runInput opens a prompt with openKey, clears any prefill, types text, and
// submits with Enter.
func runInput(model tea.Model, openKey, text string) tea.Model {
	model = press(model, openKey)
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlU}) // clear prefill (rename/due)
	for _, r := range text {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model
}

func notesCount(t *testing.T, m *Model) int {
	ns, _ := note.List(m.eng.S)
	return len(ns)
}

// TestAddNoteFromTUI is the user-reported failure: A → type → enter should
// create a note and surface it on the notes tab.
func TestAddNoteFromTUI(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	before := notesCount(t, m)

	model := runInput(m, "A", "my brand new note")
	mm := model.(*Model)

	if got := notesCount(t, mm); got != before+1 {
		t.Fatalf("add note: store has %d notes, want %d", got, before+1)
	}
	mm = press(mm, "2").(*Model) // switch to notes tab
	found := false
	for _, n := range mm.notesView {
		if n.Title == "my brand new note" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new note not visible on notes tab; notesView=%d", len(mm.notesView))
	}
}

// TestAllInputFlows exercises every prompt-based action.
func TestAllInputFlows(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	id := m.selectedTask().ID()

	// rename
	model := runInput(m, "r", "renamed task")
	mm := model.(*Model)
	if mm.eng2find(t, id).Text != "renamed task" {
		t.Errorf("rename failed: %q", mm.eng2find(t, id).Text)
	}
	// due
	model = runInput(mm, "D", "tomorrow")
	mm = model.(*Model)
	if mm.eng2find(t, id).Due() == "" {
		t.Error("set due failed")
	}
	// add tag
	model = runInput(mm, "t", "urgent")
	mm = model.(*Model)
	if !contains(mm.eng2find(t, id).Tags(), "urgent") {
		t.Error("add tag failed")
	}
	// link
	model = runInput(mm, "l", "some-note")
	mm = model.(*Model)
	if len(mm.eng2find(t, id).Links()) == 0 {
		t.Error("add link failed")
	}
	// filter
	model = runInput(mm, "/", "renamed")
	mm = model.(*Model)
	if mm.filter != "renamed" {
		t.Errorf("filter not set: %q", mm.filter)
	}
}

// TestDetailScroll: enter focuses the detail pane and j/k scroll its body
// (the long-note scroll fix) without moving the list selection.
func TestDetailScroll(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	body := strings.Repeat("a line of note body\n", 80)
	if _, err := note.Create(m.eng.S, "Long Note", body, nil, "tui"); err != nil {
		t.Fatal(err)
	}
	m.reload()
	m.tab = tabNotes
	m.cursor = 0
	beforeNote := m.selectedNote().Title

	mm := press(m, "enter").(*Model)
	if !mm.detailFocus {
		t.Fatal("enter should focus the detail pane")
	}
	mm = press(mm, "j", "j", "j").(*Model)
	mm.View() // render clamps the scroll
	if mm.detailScroll == 0 {
		t.Fatal("j should scroll the detail body when focused")
	}
	if mm.selectedNote().Title != beforeNote {
		t.Fatal("scrolling the detail must not change the list selection")
	}
	mm = press(mm, "esc").(*Model)
	if mm.detailFocus {
		t.Fatal("esc should unfocus the detail pane")
	}
}

// TestFollowLink: L on a task with a [[note]] link jumps to that note.
func TestFollowLink(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	n, err := note.Create(m.eng.S, "Target Note", "body", nil, "tui")
	if err != nil {
		t.Fatal(err)
	}
	slug := strings.TrimSuffix(filepathBase(n.Path), ".md")
	model := runInput(m, "A", "ignore") // unrelated, just to have activity
	mm := model.(*Model)
	mm.tab = tabTasks
	mm.cursor = 0
	// link the selected task to the note, then follow it.
	id := mm.selectedTask().ID()
	mm.addLinkTarget(slug)
	mm.selectByID(id)
	mm = press(mm, "L").(*Model)
	if mm.tab != tabNotes {
		t.Fatal("L should switch to the notes tab")
	}
	if sn := mm.selectedNote(); sn == nil || sn.Title != "Target Note" {
		t.Fatalf("L should select the linked note, got %v", sn)
	}
}

func filepathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// TestImmediateActions exercises the non-prompt keys.
func TestImmediateActions(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	id := m.selectedTask().ID()

	// priority cycle: none -> A
	mm := press(m, "p").(*Model)
	if mm.eng2find(t, id).Priority != 'A' {
		t.Errorf("priority cycle failed: %q", mm.eng2find(t, id).Priority)
	}
	// group cycle
	mm = press(mm, "v").(*Model)
	if mm.grp != groupProject {
		t.Errorf("group cycle failed: %v", mm.grp)
	}
	// show done toggle
	mm = press(mm, ".").(*Model)
	if !mm.showDone {
		t.Error("show-done toggle failed")
	}
	// show blocked toggle
	mm = press(mm, "b").(*Model)
	if !mm.showBlocked {
		t.Error("show-blocked toggle failed")
	}
	// done then undo
	mm = press(mm, "x").(*Model)
	if !mm.eng2find(t, id).Done {
		t.Error("x done failed")
	}
	mm = press(mm, "u").(*Model)
	if mm.eng2find(t, id).Done {
		t.Error("undo failed")
	}
}

// TestEscCancelsInput verifies esc aborts a prompt without mutating.
func TestEscCancelsInput(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	before := m.selectedTask().Text
	var model tea.Model = m
	model = press(model, "r")
	for _, r := range "should not apply" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := model.(*Model)
	if mm.selectedTask().Text != before {
		t.Errorf("esc should cancel rename; got %q", mm.selectedTask().Text)
	}
	if mm.ik != inNone {
		t.Error("esc should exit input mode")
	}
}

// TestRenderAfterEachAction makes sure View doesn't panic mid-flow.
func TestRenderAfterEachAction(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	var model tea.Model = m
	for _, k := range []string{"j", "x", "u", "v", "b", ".", "p", "2", "1", "G", "g"} {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		if strings.TrimSpace(model.(*Model).View()) == "" {
			t.Fatalf("empty view after %q", k)
		}
	}
}
