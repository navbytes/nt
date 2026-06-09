package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteCmd is one action offered by the : command palette (U2). run performs it
// (returning a tea.Cmd when it opens a prompt). write marks actions that mutate
// the store, so they can be blocked under the read-only lock.
type paletteCmd struct {
	name  string
	desc  string
	write bool
	run   func(m *Model) tea.Cmd
}

// paletteCommands is the action registry the palette filters over. It mirrors the
// keybindings so the TUI is discoverable without memorizing them.
func paletteCommands() []paletteCmd {
	prompt := func(ik inputKind, placeholder string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.startInput(ik, "", placeholder) }
	}
	return []paletteCmd{
		{"add task", "create a task", true, prompt(inAddTask, "new task")},
		{"add note", "create a note", true, prompt(inAddNote, "new note title")},
		{"toggle done", "complete / reopen the selected task", true, func(m *Model) tea.Cmd { m.toggleDone(); return nil }},
		{"toggle doing", "mark the task in-progress", true, func(m *Model) tea.Cmd { m.toggleDoing(); return nil }},
		{"delete", "delete the marked tasks (undoable)", true, func(m *Model) tea.Cmd { m.deleteTargets(); return nil }},
		{"set priority", "set priority for the selection", true, prompt(inSetPri, "priority high/med/low/none")},
		{"set due date", "set the due date", true, prompt(inDue, "due (today, fri, +3d, YYYY-MM-DD)")},
		{"add tag", "add a @tag", true, prompt(inTag, "add tag")},
		{"add link", "add a [[link]]", true, prompt(inLink, "link target")},
		{"undo", "revert the last change", true, func(m *Model) tea.Cmd { m.undo(); return nil }},
		{"redo", "re-apply the last undone change", true, func(m *Model) tea.Cmd { m.redo(); return nil }},
		{"archive done", "move completed tasks to done.txt", true, func(m *Model) tea.Cmd { m.archiveDone(); return nil }},
		{"archive note", "retire / restore the selected note (reversible)", true, func(m *Model) tea.Cmd { m.archiveNote(); return nil }},
		{"filter", "filter the list", false, prompt(inFilter, "filter")},
		{"follow link/tag", "pick a token to open or scope", false, func(m *Model) tea.Cmd { m.startFollow(); return nil }},
		{"go to tasks", "switch to the tasks tab", false, func(m *Model) tea.Cmd { m.tab, m.cursor, m.offset = tabTasks, 0, 0; return nil }},
		{"go to notes", "switch to the notes tab", false, func(m *Model) tea.Cmd { m.tab, m.cursor, m.offset = tabNotes, 0, 0; return nil }},
		{"go to logbook", "switch to the logbook tab", false, func(m *Model) tea.Cmd { m.tab, m.cursor, m.offset = tabLogbook, 0, 0; return nil }},
		{"group by date", "group tasks by due date", false, func(m *Model) tea.Cmd { m.grp = groupDate; m.rebuild(); return nil }},
		{"group by project", "group tasks by project", false, func(m *Model) tea.Cmd { m.grp = groupProject; m.rebuild(); return nil }},
		{"group by tag", "group tasks by tag", false, func(m *Model) tea.Cmd { m.grp = groupTag; m.rebuild(); return nil }},
		{"toggle done visibility", "show / hide completed tasks", false, func(m *Model) tea.Cmd { m.showDone = !m.showDone; m.rebuild(); return nil }},
		{"toggle blocked visibility", "show / hide dependency-blocked tasks", false, func(m *Model) tea.Cmd { m.showBlocked = !m.showBlocked; m.rebuild(); return nil }},
		{"toggle archived visibility", "show / hide retired notes (notes tab)", false, func(m *Model) tea.Cmd { m.showArchived = !m.showArchived; m.rebuild(); return nil }},
		{"help", "show the key reference", false, func(m *Model) tea.Cmd { m.help = true; m.helpScroll = 0; return nil }},
	}
}

// filteredPalette returns the commands matching the current query (case-
// insensitive substring on name or description), preserving registry order.
func (m *Model) filteredPalette() []paletteCmd {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	all := paletteCommands()
	if q == "" {
		return all
	}
	var out []paletteCmd
	for _, c := range all {
		if strings.Contains(c.name, q) || strings.Contains(c.desc, q) {
			out = append(out, c)
		}
	}
	return out
}

// openPalette starts the : command palette.
func (m *Model) openPalette() tea.Cmd {
	m.palette = true
	m.paletteSel = 0
	m.input.SetValue("")
	m.input.Placeholder = "run a command…"
	m.input.Focus()
	return textinput.Blink
}

// updatePalette drives the palette: arrows select, enter runs, esc cancels, other
// keys edit the filter.
func (m *Model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmds := m.filteredPalette()
	switch msg.String() {
	case "esc":
		m.closePalette()
		return m, nil
	case "up", "ctrl+k":
		if m.paletteSel > 0 {
			m.paletteSel--
		}
		return m, nil
	case "down", "ctrl+j":
		if m.paletteSel < len(cmds)-1 {
			m.paletteSel++
		}
		return m, nil
	case "enter":
		if m.paletteSel >= len(cmds) {
			m.closePalette()
			return m, nil
		}
		c := cmds[m.paletteSel]
		m.closePalette()
		if c.write && m.locked {
			m.setStatus("locked (read-only) — ctrl+l to unlock")
			return m, nil
		}
		return m, c.run(m)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.paletteSel = 0 // re-anchor selection when the query changes
	return m, cmd
}

func (m *Model) closePalette() {
	m.palette = false
	m.input.Blur()
	m.input.SetValue("")
}

// archiveDone moves completed tasks to done.txt (the palette's "archive done").
func (m *Model) archiveDone() {
	n, err := m.eng.Archive()
	if err != nil {
		m.setStatus("archive failed: " + err.Error())
		return
	}
	m.reload()
	m.setStatus(fmt.Sprintf("archived %d completed task(s)", n))
}

// paletteView renders the command palette overlay (input + filtered list).
func (m *Model) paletteView() string {
	cmds := m.filteredPalette()
	var b strings.Builder
	b.WriteString(stSec.Render("  : command palette") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	if len(cmds) == 0 {
		b.WriteString(stDim.Render("  no matching command") + "\n")
	}
	max := 12
	for i, c := range cmds {
		if i >= max {
			b.WriteString(stDim.Render(fmt.Sprintf("  … %d more", len(cmds)-max)) + "\n")
			break
		}
		name := c.name
		if c.write && m.locked {
			name += " (locked)"
		}
		line := fmt.Sprintf("  %-26s %s", name, c.desc)
		if i == m.paletteSel {
			b.WriteString(lipgloss.NewStyle().Foreground(cFg).Background(cSelBg).Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + stDim.Render(line) + "\n")
		}
	}
	b.WriteString("\n" + stDim.Render("  ↑↓ select · ↵ run · esc cancel"))
	return b.String()
}
