package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

type editFinishedMsg struct{ err error }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case changedMsg:
		m.reload()
		return m, waitForChange(m.changes)
	case editFinishedMsg:
		m.finishEdit()
		return m, nil
	case readyMsg:
		m.ready = true
		return m, nil
	case tea.MouseMsg:
		if m.ready {
			m.handleMouse(msg)
		}
		return m, nil
	case tea.KeyMsg:
		if !m.ready {
			return m, nil // drop startup terminal-query noise
		}
		if m.ik != inNone {
			return m.updateInput(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// A pending y/n confirmation consumes the next key.
	if m.confirm != nil {
		c := m.confirm
		m.confirm = nil
		if key == "y" || key == "Y" || key == "enter" {
			c.action()
		} else {
			m.setStatus("cancelled")
		}
		return m, nil
	}

	// Follow mode consumes the next key as a target label (or cancels).
	if m.followMode {
		m.handleFollowKey(key)
		return m, nil
	}

	// Clear the pending-'d' hint (and its status) on any other key. The status
	// line is NOT wiped on every key, so action feedback persists until replaced.
	if key != "d" && m.pendD {
		m.pendD = false
		m.status = ""
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.detailFocus {
			m.detailScroll++
		} else {
			m.move(1)
		}
	case "k", "up":
		if m.detailFocus {
			m.detailScroll--
		} else {
			m.move(-1)
		}
	case "ctrl+d":
		if m.detailFocus {
			m.detailScroll += m.halfPage()
		} else {
			m.move(m.halfPage())
		}
	case "ctrl+u":
		if m.detailFocus {
			m.detailScroll -= m.halfPage()
		} else {
			m.move(-m.halfPage())
		}
	case "g", "home":
		if m.detailFocus {
			m.detailScroll = 0
		} else {
			m.cursor, m.offset = 0, 0
		}
	case "G", "end":
		if m.detailFocus {
			m.detailScroll = 1 << 20 // clamped to the bottom on render
		} else {
			m.cursor = m.selectableLen() - 1
			m.clampCursor()
		}
	case "1":
		m.tab, m.cursor, m.offset = tabTasks, 0, 0
	case "2":
		m.tab, m.cursor, m.offset = tabNotes, 0, 0
	case "tab":
		m.tab = tabTasks + tabNotes - m.tab
		m.cursor, m.offset = 0, 0
	case "v":
		m.grp = (m.grp + 1) % 3
		m.rebuild()
		m.setStatus("group: " + m.grp.String())
	case ".":
		m.showDone = !m.showDone
		m.rebuild()
	case "b":
		m.showBlocked = !m.showBlocked
		m.rebuild()
		m.setStatus("blocked: " + onOff(m.showBlocked))
	case "enter":
		// Focus the detail pane so j/k scroll its body (esp. long note bodies).
		m.detailFocus = !m.detailFocus
		m.detailScroll = 0
		if m.detailFocus {
			m.setStatus("detail focused — j/k scroll · esc back")
		}
	case "esc":
		switch {
		case m.detailFocus:
			m.detailFocus = false
			m.status = ""
		case m.visualMode || len(m.marked) > 0:
			m.clearMarks()
		case m.help:
			m.help = false
		case m.filter != "":
			m.filter = ""
			m.rebuild()
			m.setStatus("filter cleared")
		default:
			m.clearScope()
		}
	case " ", "space":
		m.toggleMark()
	case "V":
		m.startVisual()
	case "f":
		m.startFollow()
	case "x":
		m.toggleDone()
	case "X":
		m.deleteTargets()
	case "d":
		if m.pendD {
			m.pendD = false
			m.status = ""
			m.toggleDone()
		} else {
			m.pendD = true
			m.setStatus("d-  press d again to toggle done")
		}
	case "u":
		m.undo()
	case "p":
		if len(m.marked) > 0 {
			return m, m.startInput(inSetPri, "", "priority for marked: high/med/low/none")
		}
		m.cyclePriority()
	case "L":
		m.followLink()
	case "?":
		m.help = !m.help

	// prompt-opening keys
	case "a":
		return m, m.startInput(inAddTask, "", "new task")
	case "A":
		return m, m.startInput(inAddNote, "", "new note title")
	case "/":
		return m, m.startInput(inFilter, m.filter, "filter")
	case "r":
		if t := m.selectedTask(); t != nil {
			return m, m.startInput(inRename, t.Text, "rename")
		}
	case "D":
		if t := m.selectedTask(); t != nil {
			return m, m.startInput(inDue, t.Due(), "due (today, fri, +3d, YYYY-MM-DD)")
		}
	case "t":
		if m.selectedTask() != nil {
			return m, m.startInput(inTag, "", "add tag")
		}
	case "T":
		if t := m.selectedTask(); t != nil && len(t.Tags()) > 0 {
			return m, m.startInput(inUntag, "", "remove tag: "+strings.Join(t.Tags(), " "))
		}
	case "l":
		if m.selectedTask() != nil {
			return m, m.startInput(inLink, "", "link target (note slug or task id)")
		}
	case "e", "E":
		return m.startEdit()
	}
	return m, nil
}

func (m *Model) move(d int) {
	n := m.selectableLen()
	if n == 0 {
		return
	}
	m.cursor += d
	m.clampCursor()
	m.detailScroll = 0 // new selection → reset the detail pane scroll
	m.paintVisual()    // extend the V range as the cursor moves
}

func (m *Model) startInput(ik inputKind, initial, placeholder string) tea.Cmd {
	m.ik = ik
	m.input.SetValue(initial)
	m.input.Placeholder = placeholder
	m.input.CursorEnd()
	m.input.Focus()
	return textinput.Blink
}

func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ik = inNone
		m.input.Blur()
		return m, nil
	case "enter":
		return m.commitInput()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) commitInput() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.input.Value())
	ik := m.ik
	m.ik = inNone
	m.input.Blur()
	switch ik {
	case inAddTask:
		if val != "" {
			m.addTask(val)
		}
	case inAddNote:
		if val != "" {
			m.addNote(val)
		}
	case inFilter:
		m.filter = val
		m.offset = 0
		m.rebuild()
		if val != "" {
			m.setStatus(fmt.Sprintf("%d match(es)", m.selectableLen()))
		}
	case inRename:
		if val != "" {
			m.rename(val)
		}
	case inDue:
		m.setDue(val)
	case inTag:
		if val != "" {
			m.addTag(val)
		}
	case inUntag:
		if val != "" {
			m.removeTag(val)
		}
	case inSetPri:
		if p, ok := dateparse.Priority(val); ok {
			m.setPriority(p)
		} else {
			m.setStatus("invalid priority")
		}
	case inLink:
		if val != "" {
			m.addLinkTarget(val)
		}
	}
	return m, nil
}

// --- actions (all mutate by ULID through the shared engine, then reload) --

func (m *Model) addTask(text string) {
	var id string
	_ = m.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		t := task.New(text)
		t.SetKey("src", "tui")
		d.Append(t)
		rec.Added(t)
		id = t.ID()
		return nil
	})
	m.reload()
	m.selectTaskByID(id)
	m.setStatus("added")
}

func (m *Model) addNote(title string) {
	n, err := note.Create(m.eng.S, title, "", nil, "tui")
	if err != nil {
		m.setStatus("note error")
		return
	}
	// Switch to the notes tab and select the new note so it's visible (otherwise
	// adding a note from the tasks tab looks like nothing happened).
	m.reload()
	m.tab = tabNotes
	m.filter = ""
	m.cursor = 0
	m.rebuild()
	m.selectByID(n.ID)
	m.setStatus("note added — press e to edit its body")
}

func (m *Model) toggleDone() {
	if len(m.marked) > 0 {
		m.bulkDone() // complete the marked set (confirms if recurring/hidden)
		return
	}
	t := m.selectedTask()
	if t == nil {
		return
	}
	id := t.ID()
	if t.Done {
		m.mutate("reopen", id, func(tk *task.Task) { tk.SetDone(false, "") })
		return
	}
	// Completing may spawn a recurrence; do it as one transaction (SPEC §9).
	_ = m.eng.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		if tk := d.FindByID(id); tk != nil {
			mutate.Complete(d, rec, tk, mutate.Today())
		}
		return nil
	})
	m.reload()
	m.selectTaskByID(id)
}

func (m *Model) cyclePriority() {
	t := m.selectedTask()
	if t == nil {
		return
	}
	id, next := t.ID(), nextPri(t.Priority)
	m.mutate("priority", id, func(tk *task.Task) { tk.SetPriority(next) })
}

func (m *Model) rename(text string) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	m.mutate("rename", t.ID(), func(tk *task.Task) { tk.SetText(text) })
}

func (m *Model) setDue(val string) {
	d, ok := dateparse.Date(val)
	if !ok {
		m.setStatus("invalid date")
		return
	}
	if n := m.applyToTargets("due", func(tk *task.Task) { tk.SetKey("due", d) }); n > 1 {
		m.setStatus(fmt.Sprintf("due set on %d", n))
	}
}

// setPriority sets an absolute priority on the marked set (bulk `p` prompts a
// letter; single `p` cycles via cyclePriority).
func (m *Model) setPriority(p byte) {
	if n := m.applyToTargets("priority", func(tk *task.Task) { tk.SetPriority(p) }); n > 1 {
		m.setStatus(fmt.Sprintf("priority set on %d", n))
	}
}

func (m *Model) addTag(tag string) {
	tag = strings.TrimPrefix(tag, "@")
	if tag == "" {
		return
	}
	if n := m.applyToTargets("tag", func(tk *task.Task) {
		if !contains(tk.Tags(), tag) {
			tk.SetText(tk.Text + " @" + tag)
		}
	}); n > 1 {
		m.setStatus(fmt.Sprintf("tagged %d", n))
	}
}

func (m *Model) removeTag(tag string) {
	tag = strings.TrimPrefix(tag, "@")
	m.applyToTargets("untag", func(tk *task.Task) {
		words := strings.Fields(tk.Text)
		out := words[:0]
		for _, w := range words {
			if w != "@"+tag {
				out = append(out, w)
			}
		}
		tk.SetText(strings.Join(out, " "))
	})
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func (m *Model) addLinkTarget(target string) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	target = strings.Trim(target, "[]")
	m.mutate("link", t.ID(), func(tk *task.Task) { tk.AddLink(target) })
}

func (m *Model) undo() {
	if op, did, err := m.eng.Undo(); err == nil && did {
		m.setStatus("undid: " + op)
	} else {
		m.setStatus("nothing to undo")
	}
	m.reload()
}

// mutate applies a ULID-keyed change to one task and reloads the view.
func (m *Model) mutate(op, id string, fn func(*task.Task)) {
	_ = m.eng.Apply(op, func(d *task.Doc, rec *mutate.Recorder) error {
		tk := d.FindByID(id)
		if tk == nil {
			return nil
		}
		rec.Before(tk)
		fn(tk)
		return nil
	})
	m.reload()
	m.selectTaskByID(id)
}

func (m *Model) followLink() {
	t := m.selectedTask()
	if t == nil {
		return
	}
	ls := t.Links()
	if len(ls) == 0 {
		m.setStatus("no links")
		return
	}
	d, err := m.eng.Read()
	if err != nil {
		return
	}
	it, ok := links.Resolve(ls[0], d, m.notes)
	if !ok {
		m.setStatus("unresolved link")
		return
	}
	if it.Kind == "note" {
		m.tab = tabNotes
	} else {
		m.tab = tabTasks
	}
	m.filter = "" // clear filter so the target is visible
	m.rebuild()
	m.selectByID(it.ID)
	m.setStatus("→ " + it.Title)
}

func (m *Model) selectTaskByID(id string) {
	for i, t := range m.flat {
		if t.ID() == id {
			m.cursor = i
			return
		}
	}
}

func nextPri(p byte) byte {
	switch p {
	case 0:
		return 'A'
	case 'A':
		return 'B'
	case 'B':
		return 'C'
	default:
		return 0
	}
}

// --- external editor (SPEC §6.2: never hand $EDITOR the shared file) ------

func (m *Model) startEdit() (tea.Model, tea.Cmd) {
	if m.tab == tabNotes {
		if n := m.selectedNote(); n != nil {
			return m, m.execEditor(n.Path)
		}
		return m, nil
	}
	t := m.selectedTask()
	if t == nil {
		return m, nil
	}
	tmp, err := os.CreateTemp("", "nt-edit-*.txt")
	if err != nil {
		m.setStatus("edit error")
		return m, nil
	}
	tmp.WriteString(t.Line() + "\n")
	tmp.Close()
	m.editTmp, m.editID = tmp.Name(), t.ID()
	return m, m.execEditor(m.editTmp)
}

func (m *Model) execEditor(path string) tea.Cmd {
	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = "vi"
	}
	c := exec.Command(ed, path)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editFinishedMsg{err} })
}

func (m *Model) finishEdit() {
	if m.editID != "" && m.editTmp != "" {
		data, _ := os.ReadFile(m.editTmp)
		os.Remove(m.editTmp)
		id := m.editID
		m.editID, m.editTmp = "", ""
		if line := firstNonEmptyLine(string(data)); line != "" {
			if nt, ok := task.ParseLine(line); ok {
				if nt.ID() == "" {
					nt.SetKey("id", id)
				}
				_ = m.eng.Apply("edit", func(d *task.Doc, rec *mutate.Recorder) error {
					old := d.FindByID(id)
					if old == nil {
						return nil
					}
					rec.Before(old)
					d.ReplaceByID(id, nt)
					return nil
				})
			}
		}
	}
	m.reload()
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}
