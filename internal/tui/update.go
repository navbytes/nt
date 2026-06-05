package tui

import (
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
	if key != "d" {
		m.pendD = false
	}
	m.status = ""

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = m.selectableLen() - 1
		m.clampCursor()
	case "1":
		m.tab, m.cursor = tabTasks, 0
	case "2":
		m.tab, m.cursor = tabNotes, 0
	case "tab":
		m.tab = tabTasks + tabNotes - m.tab
		m.cursor = 0
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
		m.detail = !m.detail
	case "esc":
		m.detail, m.help = false, false
	case "d":
		if m.pendD {
			m.pendD = false
			m.toggleDone()
		} else {
			m.pendD = true
		}
	case "u":
		m.undo()
	case "p":
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
		m.rebuild()
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
	if _, err := note.Create(m.eng.S, title, "", nil, "tui"); err != nil {
		m.setStatus("note error")
		return
	}
	m.reload()
	m.setStatus("note created")
}

func (m *Model) toggleDone() {
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
	t := m.selectedTask()
	if t == nil {
		return
	}
	d, ok := dateparse.Date(val)
	if !ok {
		m.setStatus("invalid date")
		return
	}
	m.mutate("due", t.ID(), func(tk *task.Task) { tk.SetKey("due", d) })
}

func (m *Model) addTag(tag string) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	tag = strings.TrimPrefix(tag, "@")
	m.mutate("tag", t.ID(), func(tk *task.Task) { tk.SetText(tk.Text + " @" + tag) })
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
		m.selectNoteByID(it.ID)
	} else {
		m.tab = tabTasks
		m.selectTaskByID(it.ID)
	}
}

func (m *Model) selectTaskByID(id string) {
	for i, t := range m.flat {
		if t.ID() == id {
			m.cursor = i
			return
		}
	}
}

func (m *Model) selectNoteByID(id string) {
	for i, n := range m.notes {
		if n.ID == id {
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
