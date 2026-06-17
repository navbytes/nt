package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/quickadd"
	"github.com/navbytes/nt/internal/task"
)

type editFinishedMsg struct{ err error }

// statusClearMsg fires a few seconds after a status was set; it clears the line
// only if no newer status replaced it in the meantime (gen still matches).
type statusClearMsg struct{ gen int }

// statusClearDelay is how long transient status feedback lingers before it
// auto-clears, so stale messages don't sit in the footer indefinitely.
const statusClearDelay = 4 * time.Second

// scheduleStatusClear returns a command that clears the current status after a
// delay, tagged with the current generation so a later status cancels it.
func (m *Model) scheduleStatusClear() tea.Cmd {
	gen := m.statusGen
	return tea.Tick(statusClearDelay, func(time.Time) tea.Msg {
		return statusClearMsg{gen: gen}
	})
}

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
	case statusClearMsg:
		if msg.gen == m.statusGen {
			m.status = ""
		}
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
		genBefore := m.statusGen
		var (
			mod tea.Model
			cmd tea.Cmd
		)
		switch {
		case m.bodyEdit:
			mod, cmd = m.updateBodyEdit(msg)
		case m.palette:
			mod, cmd = m.updatePalette(msg)
		case m.ik != inNone:
			mod, cmd = m.updateInput(msg)
		default:
			mod, cmd = m.updateNormal(msg)
		}
		// If this key set a fresh, non-empty status, schedule its auto-clear so
		// stale transient feedback doesn't linger in the footer.
		if m.statusGen != genBefore && m.status != "" {
			cmd = tea.Batch(cmd, m.scheduleStatusClear())
		}
		return mod, cmd
	}
	return m, nil
}

func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// The help overlay captures all keys: scroll it, or close on ? / esc / q.
	if m.help {
		m.handleHelpKey(key)
		return m, nil
	}

	// A pending confirmation consumes the next key.
	if m.confirm != nil {
		c := m.confirm
		m.confirm = nil
		switch {
		case c.choices != nil: // multi-choice (e.g. delete-with-backlinks)
			if act, ok := c.choices[strings.ToLower(key)]; ok {
				act()
			} else {
				m.setStatus("cancelled")
			}
		case key == "y" || key == "Y" || key == "enter":
			c.action()
		default:
			m.setStatus("cancelled")
		}
		return m, nil
	}

	// A yank chord consumes the next key as the target (y/l/t).
	if m.yankPending {
		m.handleYankKey(key)
		return m, nil
	}

	// Follow mode consumes the next key as a target label (or cancels).
	if m.followMode {
		m.handleFollowKey(key)
		return m, nil
	}

	// Vim count prefix: 1-9 (and 0 after a non-zero) accumulate a repeat count the
	// next motion consumes (e.g. 5j, 12G). The tab keys moved to [ ] to free the
	// digits. Only in the list (not while scrolling the detail pane).
	if !m.detailFocus && (len(key) == 1 && key[0] >= '1' && key[0] <= '9' || (key == "0" && m.count > 0)) {
		m.count = min(m.count*10+int(key[0]-'0'), 99999)
		m.setStatus(fmt.Sprintf("%d…", m.count))
		return m, nil
	}
	// Any other key consumes the count: n is the repeat (≥1), hadCount records
	// whether one was actually typed (for G's "go to line N" vs "go to end").
	hadCount := m.count > 0
	n := m.count
	m.count = 0
	if n < 1 {
		n = 1
	}

	// Clear the pending-'d' hint (and its status) on any other key. The status
	// line is NOT wiped on every key, so action feedback persists until replaced.
	if key != "d" && m.pendD {
		m.pendD = false
		m.status = ""
	}

	// Read-only lock: swallow any mutating key until unlocked (ctrl+l).
	// Navigation, filter/scope, grouping, follow, and yank stay available.
	if m.locked && isWriteKey(key) {
		m.setStatus("locked (read-only) — ctrl+l to unlock")
		return m, nil
	}

	switch key {
	case "ctrl+l":
		m.locked = !m.locked
		if m.locked {
			m.setStatus("locked — writes disabled (ctrl+l to unlock)")
		} else {
			m.setStatus("unlocked — writes enabled")
		}
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		// Back out of any active view state first; quit only from a clean list,
		// so a stray q can't drop you out mid-filter/detail/selection.
		if !m.backOut() {
			return m, tea.Quit
		}
	case "j", "down":
		if m.detailFocus {
			m.detailScroll++
		} else {
			m.move(n) // n = vim count (default 1)
		}
	case "k", "up":
		if m.detailFocus {
			m.detailScroll--
		} else {
			m.move(-n)
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
		switch {
		case m.detailFocus:
			m.detailScroll = 1 << 20 // clamped to the bottom on render
		case hadCount: // NG → jump to the N-th row (1-based)
			m.cursor = n - 1
			m.clampCursor()
		default:
			m.cursor = m.selectableLen() - 1
			m.clampCursor()
		}
	case "]": // next tab (digits 1-3 freed for vim counts)
		m.switchTab((m.tab + 1) % tabCount)
	case "[", "shift+tab": // previous tab
		m.switchTab((m.tab + tabCount - 1) % tabCount)
	case "tab":
		m.switchTab((m.tab + 1) % tabCount)
	case "v":
		if m.tab != tabTasks {
			m.setStatus("not available here")
			break
		}
		m.grp = (m.grp + 1) % 3
		m.rebuild()
		m.setStatus("group: " + m.grp.String())
	case "<":
		m.nudgeSplit(-4) // widen the detail pane (shrink the list)
	case ">":
		m.nudgeSplit(4) // widen the list
	case ".":
		if m.tab == tabNotes {
			m.showArchived = !m.showArchived
			m.rebuild()
			m.setStatus("archived: " + onOff(m.showArchived))
		} else {
			m.showDone = !m.showDone
			m.rebuild()
		}
	case "b":
		if m.tab != tabTasks {
			m.setStatus("not available here")
			break
		}
		m.showBlocked = !m.showBlocked
		m.rebuild()
		m.setStatus("blocked: " + onOff(m.showBlocked))
	case "enter":
		// Focus the detail pane so j/k scroll its body (esp. long note bodies).
		m.detailFocus = !m.detailFocus
		m.detailScroll = 0
		if m.detailFocus {
			m.setStatus("detail focused — j/k scroll · esc back")
		} else {
			m.status = "" // un-focus clears the hint, matching esc
		}
	case "esc":
		m.backOut()
	case " ", "space":
		m.toggleMark()
	case "V":
		m.startVisual()
	case "f":
		m.startFollow()
	case "y":
		m.startYank()
	case "s":
		m.toggleDoing()
	case "x":
		if m.tab == tabNotes {
			m.archiveNote()
		} else {
			m.toggleDone()
		}
	case "X":
		if m.tab == tabNotes {
			m.deleteNoteTarget()
		} else {
			m.deleteTargets()
		}
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
	case "U":
		m.redo()
	case "p":
		if len(m.marked) > 0 {
			return m, m.startInput(inSetPri, "", "priority for marked: high/med/low/none")
		}
		m.cyclePriority()
	case "L":
		m.followLink()
	case ":":
		return m, m.openPalette()
	case "?":
		m.help = true
		m.helpScroll = 0

	// prompt-opening keys
	case "a":
		return m, m.startInput(inAddTask, "", "new task")
	case "A":
		return m, m.startInput(inAddNote, "", "new note title")
	case "/":
		// Remember the filter and the cursor/scroll position, so Esc can cancel a
		// live edit and restore exactly where you were.
		m.filterBefore = m.filter
		m.filterSelBefore = m.selectedID()
		m.filterOffBefore = m.offset
		return m, m.startInput(inFilter, m.filter, "filter")
	case "r":
		if m.tab == tabNotes {
			if n := m.selectedNote(); n != nil {
				cur := n.Rel
				if i := strings.LastIndex(cur, "/"); i >= 0 {
					cur = cur[i+1:]
				}
				return m, m.startInput(inRenameNote, strings.TrimSuffix(cur, ".md"), "rename/move note (name or folder/path)")
			}
		} else if t := m.selectedTask(); t != nil {
			return m, m.startInput(inRename, t.Text, "rename")
		}
	case "D":
		if t := m.selectedTask(); t != nil {
			return m, m.startInput(inDue, t.Due(), "due (today, fri, +3d, YYYY-MM-DD)")
		}
		m.setStatus("not available here")
	case "t":
		if m.selectedTask() != nil {
			return m, m.startInput(inTag, "", "add tag")
		}
		m.setStatus("not available here")
	case "T":
		if t := m.selectedTask(); t != nil {
			if len(t.Tags()) > 0 {
				return m, m.startInput(inUntag, "", "remove tag: "+strings.Join(t.Tags(), " "))
			}
			m.setStatus("no tags to remove")
		} else {
			m.setStatus("not available here")
		}
	case "l":
		if m.tab == tabNotes {
			if m.selectedNote() != nil {
				return m, m.startInput(inLink, "", "link to add to this note (note slug or task id)")
			}
		} else if m.selectedTask() != nil {
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
	before := m.cursor
	m.cursor += d
	m.clampCursor()
	if m.cursor != before {
		m.detailScroll = 0 // new selection → reset the detail pane scroll
	}
	m.paintVisual() // extend the V range as the cursor moves
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
		// Cancel a live filter edit: restore the filter that was active before
		// the prompt opened (search-as-you-type otherwise leaves it applied).
		if m.ik == inFilter && m.filter != m.filterBefore {
			m.filter = m.filterBefore
			m.rebuild()
			// Restore the cursor/scroll position the live filter shifted.
			if m.filterSelBefore != "" {
				m.selectByID(m.filterSelBefore)
			}
			m.offset = m.filterOffBefore
			m.clampCursor()
		}
		m.ik = inNone
		m.input.Blur()
		return m, nil
	case "enter":
		return m.commitInput()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Live filter: apply on every keystroke so the list narrows as you type —
	// the defining feel of a modern TUI filter (lazygit/aerc), versus only
	// updating on Enter.
	if m.ik == inFilter {
		if v := strings.TrimSpace(m.input.Value()); v != m.filter {
			m.filter = v
			m.offset = 0
			m.rebuild()
		}
	}
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
	case inRenameNote:
		if val != "" {
			m.renameNote(val)
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
			if m.tab == tabNotes {
				m.addLinkToNote(val)
			} else {
				m.addLinkTarget(val)
			}
		}
	}
	return m, nil
}

// --- actions (all mutate by ULID through the shared engine, then reload) --

func (m *Model) addTask(text string) {
	var id string
	_ = m.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		t := quickadd.New(text)
		t.SetKey("src", "tui")
		d.Append(t)
		rec.Added(t)
		id = t.ID()
		return nil
	})
	m.reload()
	// Surface the new task even when added from the notes/logbook tab or under a
	// filter it doesn't match — otherwise the add looks like a no-op.
	m.tab = tabTasks
	m.filter = ""
	m.cursor = 0
	m.rebuild()
	m.selectTaskByID(id)
	m.setStatus("added")
}

// toggleDoing flips the selected task between "doing" and open — the in-progress
// status the model supports but had no keybinding to reach.
func (m *Model) toggleDoing() {
	t := m.selectedTask()
	if t == nil {
		m.setStatus("not available here")
		return
	}
	id := t.ID()
	if t.State() == "doing" {
		m.mutate("update", id, func(tk *task.Task) { tk.SetState("open") })
		m.setStatus("status: open")
	} else {
		m.mutate("update", id, func(tk *task.Task) { tk.SetState("doing") })
		m.setStatus("status: doing")
	}
}

func (m *Model) addNote(title string) {
	n, err := note.Create(m.eng.S, title, "", nil, "tui", "")
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
	// Capture the body in-TUI right away (Ctrl+S to save, Esc for title-only)
	// instead of bouncing through $EDITOR (U4).
	m.openBodyCapture(n.ID, title)
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
	// A recurring task spawns its next occurrence on completion — note that now
	// so the spawn isn't silent (the bulk path confirms; this is the single path).
	recurring := t.Recur() != ""
	// Completing may spawn a recurrence; do it as one transaction (SPEC §9).
	_ = m.eng.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		if tk := d.FindByID(id); tk != nil {
			mutate.Complete(d, rec, tk, mutate.Today())
		}
		return nil
	})
	m.reload()
	m.selectTaskByID(id)
	if recurring {
		m.setStatus("done — spawned next occurrence")
	} else {
		m.setStatus("done")
	}
}

func (m *Model) cyclePriority() {
	t := m.selectedTask()
	if t == nil {
		return
	}
	id, next := t.ID(), nextPri(t.Priority)
	m.mutate("priority", id, func(tk *task.Task) { tk.SetPriority(next) })
	m.setStatus("priority: " + priLabel(next))
}

// priLabel names a priority byte for status feedback (none/high/med/low).
func priLabel(p byte) string {
	switch p {
	case 'A':
		return "high (A)"
	case 'B':
		return "med (B)"
	case 'C':
		return "low (C)"
	default:
		return "none"
	}
}

func (m *Model) rename(text string) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	m.mutate("rename", t.ID(), func(tk *task.Task) { tk.SetText(text) })
}

// renameNote renames/moves the selected note and rewrites every [[link]] to it,
// then reselects it (its ULID is unchanged).
func (m *Model) renameNote(dest string) {
	n := m.selectedNote()
	if n == nil {
		return
	}
	id := n.ID
	newRel, updated, err := m.eng.RenameNote(n, m.notes, dest)
	if err != nil {
		m.setStatus(err.Error())
		return
	}
	m.reload()
	if id != "" {
		m.selectByID(id)
	}
	m.setStatus(fmt.Sprintf("renamed → %s (%d reference(s))", newRel, updated))
}

func (m *Model) setDue(val string) {
	d, ok := dateparse.Date(val)
	if !ok {
		m.setStatus("invalid date")
		return
	}
	if n := m.applyToTargets("due", func(tk *task.Task) bool { tk.SetKey("due", d); return true }); n > 0 {
		if n == 1 {
			m.setStatus("due set")
		} else {
			m.setStatus(fmt.Sprintf("due set on %d", n))
		}
	}
}

// setPriority sets an absolute priority on the marked set (bulk `p` prompts a
// letter; single `p` cycles via cyclePriority).
func (m *Model) setPriority(p byte) {
	if n := m.applyToTargets("priority", func(tk *task.Task) bool { tk.SetPriority(p); return true }); n > 0 {
		if n == 1 {
			m.setStatus("priority: " + priLabel(p))
		} else {
			m.setStatus(fmt.Sprintf("priority set on %d", n))
		}
	}
}

func (m *Model) addTag(tag string) {
	tag = strings.TrimPrefix(tag, "@")
	if tag == "" {
		return
	}
	n := m.applyToTargets("tag", func(tk *task.Task) bool {
		if contains(tk.Tags(), tag) {
			return false // already tagged — no-op, don't count it
		}
		tk.SetText(tk.Text + " @" + tag)
		return true
	})
	switch {
	case n == 1:
		m.setStatus("tagged @" + tag)
	case n > 1:
		m.setStatus(fmt.Sprintf("tagged %d", n))
	default:
		m.setStatus("already tagged @" + tag)
	}
}

func (m *Model) removeTag(tag string) {
	tag = strings.TrimPrefix(tag, "@")
	n := m.applyToTargets("untag", func(tk *task.Task) bool {
		words := strings.Fields(tk.Text)
		out := words[:0]
		removed := false
		for _, w := range words {
			if w == "@"+tag {
				removed = true
				continue
			}
			out = append(out, w)
		}
		if !removed {
			return false
		}
		tk.SetText(strings.Join(out, " "))
		return true
	})
	switch {
	case n == 1:
		m.setStatus("removed @" + tag)
	case n > 1:
		m.setStatus(fmt.Sprintf("removed @%s from %d", tag, n))
	default:
		m.setStatus("no @" + tag + " to remove")
	}
}

// writeKeys are the keys that mutate the store; the read-only lock swallows them.
// Selection (space/V), navigation, filter/scope, grouping, follow, and yank are
// intentionally absent — they never touch disk.
var writeKeys = map[string]bool{
	"x": true, "X": true, "d": true, // done / delete / dd
	"a": true, "A": true, "r": true, "e": true, "E": true, // add / rename / edit
	"p": true, "D": true, "t": true, "T": true, "l": true, // priority / due / tag / link
	"s": true, // toggle "doing" status
	"u": true, // undo (reverses a write)
	"U": true, // redo (re-applies an undone write)
}

func isWriteKey(key string) bool { return writeKeys[key] }

// backOut pops one layer of active view state (detail focus → marks/visual →
// filter → scope), returning true if it dismissed something. Shared by `esc`
// (always backs out) and `q` (quits only when there's nothing left to back out).
// Modal states (help, confirm, yank/follow chords) are handled earlier in
// updateNormal, so they never reach here.
func (m *Model) backOut() bool {
	switch {
	case m.detailFocus:
		m.detailFocus = false
		m.status = ""
	case m.visualMode || len(m.marked) > 0:
		m.clearMarks()
	case m.filter != "":
		m.filter = ""
		m.rebuild()
		m.setStatus("filter cleared")
	case m.scopeTag != "" || m.scopeProject != "":
		m.clearScope()
	case m.viewName != "":
		m.clearView()
	default:
		return false
	}
	return true
}

// handleHelpKey scrolls the help overlay or closes it. Overscroll is clamped in
// helpView at render time (it knows the content/viewport heights).
func (m *Model) handleHelpKey(key string) {
	switch key {
	case "?", "esc", "q":
		m.help, m.helpScroll = false, 0
	case "j", "down":
		m.helpScroll++
	case "k", "up":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case "ctrl+d":
		m.helpScroll += 5
	case "ctrl+u":
		if m.helpScroll -= 5; m.helpScroll < 0 {
			m.helpScroll = 0
		}
	case "g", "home":
		m.helpScroll = 0
	case "G", "end":
		m.helpScroll = 1 << 20 // clamped on render
	}
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

// addLinkToNote appends a [[target]] wikilink to the selected note's body and
// saves it (frontmatter preserved by note.Save). The notes-tab counterpart to
// addLinkTarget — `l` was tasks-only before (U7).
func (m *Model) addLinkToNote(target string) {
	n := m.selectedNote()
	if n == nil {
		return
	}
	target = strings.Trim(target, "[]")
	if target == "" {
		return
	}
	n.Body = strings.TrimRight(n.Body, "\n") + "\n\n[[" + target + "]]\n"
	n.Updated = time.Now().Format(time.RFC3339)
	if err := n.Save(); err != nil {
		m.setStatus("link failed: " + err.Error())
		return
	}
	m.reload()
	m.setStatus("linked → [[" + target + "]]")
}

// archiveNote toggles the soft `archived` flag on the selected note — the TUI
// face of the same reversible retire the CLI/web/MCP offer. An archived note
// drops out of the notes list (press '.' to reveal the retired set) but stays on
// disk with its links intact.
func (m *Model) archiveNote() {
	n := m.selectedNote()
	if n == nil {
		return
	}
	n.Archived = !n.Archived
	n.Updated = time.Now().Format(time.RFC3339)
	verb, title := "archived", n.Title
	if !n.Archived {
		verb = "unarchived"
	}
	if err := n.Save(); err != nil {
		m.setStatus("archive failed: " + err.Error())
		return
	}
	m.reload()
	m.setStatus(verb + " “" + title + "”")
}

// undo reverses the last forward write. The journal is a single-entry toggle
// (an undo leaves a redo behind), so undo and redo are split by direction: undo
// only fires on a fresh forward op, redo only on a pending redo (see redo).
func (m *Model) undo() {
	label, isRedo, ok := m.eng.PeekUndo()
	if !ok {
		m.setStatus("nothing to undo")
		return
	}
	if isRedo {
		m.setStatus("nothing to undo — press U to redo " + label)
		return
	}
	if _, did, err := m.eng.Undo(); err != nil {
		m.setStatus("undo failed: " + err.Error())
	} else if did {
		m.setStatus("undid: " + label + " — U to redo")
	}
	m.reload()
}

// redo re-applies the most recently undone write (the pending redo entry).
func (m *Model) redo() {
	label, isRedo, ok := m.eng.PeekUndo()
	if !ok || !isRedo {
		m.setStatus("nothing to redo")
		return
	}
	if _, did, err := m.eng.Undo(); err != nil { // Undo() toggles: applying a redo entry redoes it
		m.setStatus("redo failed: " + err.Error())
	} else if did {
		m.setStatus("redid: " + label + " — u to undo")
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

// followLink (L) jumps to a [[link]] on the selected item. With one link it
// follows it directly; with several it opens the follow-mode picker scoped to
// just the links, so you choose which to follow (SPEC §11.2) instead of silently
// taking the first.
func (m *Model) followLink() {
	targets := m.linkTargets()
	switch len(targets) {
	case 0:
		m.setStatus("no links")
	case 1:
		m.gotoLink(targets[0].value)
	default:
		m.followTargets = targets
		m.followMode = true
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
