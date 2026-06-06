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

// pickTask selects a task that has both a tag and a project; returns it.
func selectTagProjTask(m *Model) (tag, proj string, ok bool) {
	for i, tk := range m.flat {
		if len(tk.Tags()) > 0 && len(tk.Projects()) > 0 {
			m.cursor = i
			return tk.Tags()[0], tk.Projects()[0], true
		}
	}
	return "", "", false
}

func labelFor(m *Model, kind, value string) string {
	for _, ft := range m.followTargets {
		if ft.kind == kind && ft.value == value {
			return string(ft.label)
		}
	}
	return ""
}

// TestFollowScopeTag: f then the tag's label scopes the list to that tag.
func TestFollowScopeTag(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	tag, _, ok := selectTagProjTask(m)
	if !ok {
		t.Skip("no tagged+projected task in fixture")
	}
	mm := press(m, "f").(*Model)
	if !mm.followMode {
		t.Fatal("f should enter follow mode")
	}
	lbl := labelFor(mm, "tag", tag)
	mm = press(mm, lbl).(*Model)
	if mm.followMode {
		t.Fatal("follow mode should exit after a pick")
	}
	if mm.scopeTag != tag {
		t.Fatalf("scope tag: got %q want %q", mm.scopeTag, tag)
	}
	// every visible task should now carry the tag
	for _, tk := range mm.flat {
		if !contains(tk.Tags(), tag) {
			t.Fatalf("scoped list has a task without @%s", tag)
		}
	}
}

// TestFollowGroupProject: f then the UPPERCASE project label regroups by project.
func TestFollowGroupProject(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	_, proj, ok := selectTagProjTask(m)
	if !ok {
		t.Skip("no suitable task")
	}
	mm := press(m, "f").(*Model)
	lbl := labelFor(mm, "project", proj)
	mm = press(mm, strings.ToUpper(lbl)).(*Model)
	if mm.grp != groupProject {
		t.Fatalf("uppercase project label should regroup by project, got %v", mm.grp)
	}
	if mm.scopeProject != "" {
		t.Fatal("group variant should not also scope")
	}
}

// TestEscClearsScope: esc clears an active scope.
func TestEscClearsScope(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	m.scopeTag = "backend"
	m.rebuild()
	mm := press(m, "esc").(*Model)
	if mm.scopeTag != "" {
		t.Fatal("esc should clear the scope")
	}
}

// TestMouseClickActivatesToken: clicking a @tag token scopes the list.
func TestMouseClickActivatesToken(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 130, 30 // wide split
	m.View()                    // populate the click hit-map
	var clickX, clickY int
	var wantTag string
	for li, hl := range m.hitLines {
		for _, sp := range hl.tokens {
			if sp.ft.kind == "tag" {
				clickX, clickY, wantTag = sp.start, 2+(li-m.offset), sp.ft.value
			}
		}
		if wantTag != "" {
			break
		}
	}
	if wantTag == "" {
		t.Skip("no clickable tag token in fixture")
	}
	var model tea.Model = m
	model, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: clickX, Y: clickY})
	if mm := model.(*Model); mm.scopeTag != wantTag {
		t.Fatalf("clicking @%s should scope to it; got %q", wantTag, mm.scopeTag)
	}
}

// TestLogbookShowsCompleted: completing a task moves it into the Logbook tab;
// reopening it from there removes it again.
func TestLogbookShowsCompleted(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	if len(m.logFlat) != 0 {
		t.Fatal("logbook should start empty")
	}
	m.cursor = 0
	id := m.flat[0].ID()
	mm := press(m, "x").(*Model) // complete the current task
	if len(mm.logFlat) != 1 || mm.logFlat[0].ID() != id {
		t.Fatalf("completed task should appear in the logbook, got %d entries", len(mm.logFlat))
	}
	mm = press(mm, "3").(*Model)
	if mm.tab != tabLogbook {
		t.Fatal("'3' should switch to the logbook tab")
	}
	if st := mm.selectedTask(); st == nil || st.ID() != id {
		t.Fatal("logbook selection should resolve to the completed task")
	}
	mm = press(mm, "x").(*Model) // reopen from the logbook
	if len(mm.logFlat) != 0 {
		t.Fatalf("reopening should empty the logbook, got %d", len(mm.logFlat))
	}
}

// TestSplitResize: ‹ › keys move the divider and clamp at the bounds.
func TestSplitResize(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 160, 30
	base := m.splitWidth()
	mm := press(m, ">").(*Model)
	if mm.splitWidth() <= base {
		t.Fatal("'>' should widen the list")
	}
	for i := 0; i < 40; i++ {
		mm = press(mm, "<").(*Model)
	}
	if mm.splitPct != splitMin {
		t.Fatalf("repeated '<' should clamp to %d, got %d", splitMin, mm.splitPct)
	}
	for i := 0; i < 40; i++ {
		mm = press(mm, ">").(*Model)
	}
	if mm.splitPct != splitMax {
		t.Fatalf("repeated '>' should clamp to %d, got %d", splitMax, mm.splitPct)
	}
}

// TestSplitDrag: pressing the divider starts a drag, motion resizes, release ends.
func TestSplitDrag(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 160, 30
	div := m.splitWidth()
	var model tea.Model = m
	model, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: div, Y: 5})
	if !model.(*Model).draggingSplit {
		t.Fatal("pressing the divider column should start a drag")
	}
	model, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 64, Y: 5})
	if got := model.(*Model).splitPct; got != 40 {
		t.Fatalf("drag to col 64/160 should be 40%%, got %d", got)
	}
	model, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 64, Y: 5})
	if model.(*Model).draggingSplit {
		t.Fatal("release should end the drag")
	}
}

// TestKeybarContext: the footer hints adapt to the tab and the selection.
func TestKeybarContext(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	has := func(pairs [][2]string, label string) bool {
		for _, p := range pairs {
			if p[1] == label {
				return true
			}
		}
		return false
	}

	m.tab, m.cursor = tabTasks, 0
	if tp := m.keybarPairs(); !has(tp, "due") || !has(tp, "done") {
		t.Fatal("tasks keybar should show due + done")
	}
	// selection with tokens shows follow; a tokenless one does not.
	if !has(m.keybarPairs(), "follow") {
		t.Fatal("a task with @tag/+project should offer follow")
	}
	m.cursor = 2 // "deploy" — no tokens
	if has(m.keybarPairs(), "follow") {
		t.Fatal("a tokenless task should not offer follow")
	}

	m.tab, m.cursor = tabNotes, 0
	np := m.keybarPairs()
	if has(np, "due") || has(np, "done") {
		t.Fatal("notes keybar should not show task-only keys")
	}
	if !has(np, "add note") {
		t.Fatal("notes keybar should show add note")
	}

	done := press(testModelWide(t), "x").(*Model) // complete a task → logbook
	done.tab, done.cursor = tabLogbook, 0
	lp := done.keybarPairs()
	if !has(lp, "reopen") || has(lp, "due") {
		t.Fatal("logbook keybar should show reopen and not due")
	}
}

func testModelWide(t *testing.T) *Model {
	m := testModel(t)
	m.width, m.height = 100, 24
	m.cursor = 0
	return m
}

// TestClickTabSwitch: clicking the tab labels in the header switches tabs.
func TestClickTabSwitch(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 120, 30
	m.View() // populate tabHits
	if len(m.tabHits) != 3 {
		t.Fatalf("expected 3 tab hits, got %d", len(m.tabHits))
	}
	clickTab := func(model tea.Model, i int) tea.Model {
		th := model.(*Model).tabHits[i]
		model.(*Model).View() // refresh tabHits for the current tab
		out, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: th.start + 1, Y: 0})
		return out
	}
	var model tea.Model = m
	if model = clickTab(model, 1); model.(*Model).tab != tabNotes {
		t.Fatal("clicking the notes label should switch to the notes tab")
	}
	if model = clickTab(model, 2); model.(*Model).tab != tabLogbook {
		t.Fatal("clicking the log label should switch to the logbook tab")
	}
	if model = clickTab(model, 0); model.(*Model).tab != tabTasks {
		t.Fatal("clicking the tasks label should switch back")
	}
}

// TestMouseWheelScrolls: wheel events move the selection.
func TestMouseWheelScrolls(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	m.cursor = 0
	var model tea.Model = m
	model, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if model.(*Model).cursor != 1 {
		t.Fatalf("wheel down should move cursor to 1, got %d", model.(*Model).cursor)
	}
	model, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if model.(*Model).cursor != 0 {
		t.Fatalf("wheel up should move cursor back to 0, got %d", model.(*Model).cursor)
	}
}

// TestMarkAndTargets: space toggles a mark; targets() returns marks else current.
func TestMarkAndTargets(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	m.cursor = 0
	id0 := m.flat[0].ID()

	mm := press(m, " ").(*Model) // mark current
	if !mm.marked[id0] {
		t.Fatal("space should mark the current task")
	}
	if got := mm.targets(); len(got) != 1 || got[0] != id0 {
		t.Fatalf("targets should be the marked set: %v", got)
	}
	mm = press(mm, " ").(*Model) // unmark
	if mm.marked[id0] {
		t.Fatal("space should unmark")
	}
	if got := mm.targets(); len(got) != 1 || got[0] != mm.selectedTask().ID() {
		t.Fatal("with no marks, targets is the current task")
	}
}

// TestVisualRangeSelect: V then move paints a contiguous range.
func TestVisualRangeSelect(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	if len(m.flat) < 3 {
		t.Skip("need 3+ tasks")
	}
	m.cursor = 0
	mm := press(m, "V").(*Model)
	if !mm.visualMode {
		t.Fatal("V should enter visual mode")
	}
	mm = press(mm, "j", "j").(*Model) // extend over 3 rows
	if len(mm.marked) != 3 {
		t.Fatalf("visual range should mark 3, got %d", len(mm.marked))
	}
	mm = press(mm, "esc").(*Model)
	if mm.visualMode || len(mm.marked) != 0 {
		t.Fatal("esc should exit visual and clear marks")
	}
}

// TestConfirmModal: a confirmation runs its action on y, cancels otherwise.
func TestConfirmModal(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	ran := false
	m.askConfirm("Do it?", func() { ran = true })
	mm := press(m, "n").(*Model)
	if ran || mm.confirm != nil {
		t.Fatal("n should cancel the confirmation")
	}
	m.askConfirm("Do it?", func() { ran = true })
	mm = press(m, "y").(*Model)
	if !ran || mm.confirm != nil {
		t.Fatal("y should run the action and clear the confirm")
	}
}

// TestDeleteWithConfirmAndUndo: X confirms, deletes on y, and u restores.
func TestDeleteWithConfirmAndUndo(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	m.cursor = 0
	id := m.flat[0].ID()
	n0 := len(m.flat)

	mm := press(m, "X").(*Model)
	if mm.confirm == nil {
		t.Fatal("X should ask for confirmation")
	}
	mm = press(mm, "y").(*Model)
	d, _ := mm.eng.Read()
	if d.FindByID(id) != nil {
		t.Fatal("task should be deleted after confirm")
	}
	if len(mm.flat) != n0-1 {
		t.Fatalf("flat should shrink by 1, got %d want %d", len(mm.flat), n0-1)
	}
	mm = press(mm, "u").(*Model)
	d, _ = mm.eng.Read()
	if d.FindByID(id) == nil {
		t.Fatal("undo should restore the deleted task")
	}
}

// TestDeleteCancelled: X then n leaves the task.
func TestDeleteCancelled(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	id := m.selectedTask().ID()
	mm := press(m, "X", "n").(*Model)
	d, _ := mm.eng.Read()
	if d.FindByID(id) == nil {
		t.Fatal("n should cancel the delete")
	}
}

// TestBulkDelete: mark two, X, y deletes both.
func TestBulkDelete(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	if len(m.flat) < 2 {
		t.Skip("need 2+ tasks")
	}
	m.cursor = 0
	id0, id1 := m.flat[0].ID(), m.flat[1].ID()
	mm := press(m, " ", "j", " ").(*Model) // mark row 0 and row 1
	if len(mm.marked) != 2 {
		t.Fatalf("expected 2 marked, got %d", len(mm.marked))
	}
	mm = press(mm, "X", "y").(*Model)
	d, _ := mm.eng.Read()
	if d.FindByID(id0) != nil || d.FindByID(id1) != nil {
		t.Fatal("both marked tasks should be deleted")
	}
}

// TestBulkDone: x over a marked set completes all in one undoable transaction.
func TestBulkDone(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	if len(m.flat) < 2 {
		t.Skip("need 2+ tasks")
	}
	m.cursor = 0
	id0, id1 := m.flat[0].ID(), m.flat[1].ID()
	mm := press(m, " ", "j", " ").(*Model)
	mm = press(mm, "x").(*Model)
	d, _ := mm.eng.Read()
	if !d.FindByID(id0).Done || !d.FindByID(id1).Done {
		t.Fatal("bulk x should complete both marked tasks")
	}
	mm = press(mm, "u").(*Model) // one transaction → one undo reopens both
	d, _ = mm.eng.Read()
	if d.FindByID(id0).Done || d.FindByID(id1).Done {
		t.Fatal("one undo should reopen both")
	}
}

// TestBulkPriorityAbsolute: bulk p prompts and SETS an absolute priority.
func TestBulkPriorityAbsolute(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	if len(m.flat) < 2 {
		t.Skip("need 2+ tasks")
	}
	m.cursor = 0
	id0, id1 := m.flat[0].ID(), m.flat[1].ID()
	mm := press(m, " ", "j", " ").(*Model)
	mm = runInput(mm, "p", "high").(*Model) // p opens the priority prompt
	d, _ := mm.eng.Read()
	if d.FindByID(id0).Priority != 'A' || d.FindByID(id1).Priority != 'A' {
		t.Fatalf("bulk priority should set A on both; got %q %q", d.FindByID(id0).Priority, d.FindByID(id1).Priority)
	}
}

// TestBulkTag: t over a marked set tags all of them and clears the marks.
func TestBulkTag(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	if len(m.flat) < 2 {
		t.Skip("need 2+ tasks")
	}
	m.cursor = 0
	id0, id1 := m.flat[0].ID(), m.flat[1].ID()
	mm := press(m, " ", "j", " ").(*Model)
	mm = runInput(mm, "t", "sprint").(*Model)
	d, _ := mm.eng.Read()
	if !contains(d.FindByID(id0).Tags(), "sprint") || !contains(d.FindByID(id1).Tags(), "sprint") {
		t.Fatal("bulk tag should tag both")
	}
	if len(mm.marked) != 0 {
		t.Fatal("marks should clear after a bulk mutation")
	}
}

// TestYankString: the clipboard payload for id/line/text, single and bulk.
func TestYankString(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	m.cursor = 0
	t0 := m.flat[0]
	if got, want := m.yankString("id"), shortCode(t0.ID()); got != want {
		t.Errorf("yank id: got %q want %q", got, want)
	}
	if got := m.yankString("line"); got != t0.Line() {
		t.Errorf("yank line: got %q", got)
	}
	if got := m.yankString("text"); got != t0.Text {
		t.Errorf("yank text: got %q", got)
	}
	if len(m.flat) < 2 {
		return
	}
	mm := press(m, " ", "j", " ").(*Model) // mark 2
	if got := strings.Fields(mm.yankString("id")); len(got) != 2 {
		t.Errorf("bulk id yank should be 2 space-joined, got %v", got)
	}
	if got := strings.Split(mm.yankString("line"), "\n"); len(got) != 2 {
		t.Errorf("bulk line yank should be 2 newline-joined, got %d", len(got))
	}
}

// TestYankChord: y arms the chord; a non-target key cancels it.
func TestYankChord(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	mm := press(m, "y").(*Model)
	if !mm.yankPending {
		t.Fatal("y should arm the yank chord")
	}
	mm = press(mm, "g").(*Model)
	if mm.yankPending {
		t.Fatal("a non-target key should cancel the yank chord")
	}
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
