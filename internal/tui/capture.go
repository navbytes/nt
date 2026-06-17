package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openBodyCapture starts in-TUI body capture for a just-created note, replacing
// the old "press e to open $EDITOR" two-step (U4). Ctrl+S saves the body; Esc
// keeps the note title-only.
func (m *Model) openBodyCapture(noteID, title string) {
	ta := textarea.New()
	ta.Placeholder = "Write the note body…  (Ctrl+S save · Esc keep title-only)"
	ta.ShowLineNumbers = false
	ta.Prompt = "  "
	ta.CharLimit = 0
	if w := m.width - 4; w > 0 {
		ta.SetWidth(w)
	}
	if h := m.height - 6; h > 0 {
		ta.SetHeight(h)
	}
	ta.Focus()
	m.bodyArea = ta
	m.bodyNoteID = noteID
	m.bodyEdit = true
	m.setStatus("✎ " + title)
}

// updateBodyEdit routes keys while capturing a note body: Ctrl+S saves, Esc
// keeps the note title-only, everything else edits the textarea.
func (m *Model) updateBodyEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		m.saveBodyCapture()
		m.bodyEdit = false
		return m, nil
	case "esc":
		m.bodyEdit = false
		m.setStatus("note added (title only)")
		return m, nil
	}
	var cmd tea.Cmd
	m.bodyArea, cmd = m.bodyArea.Update(msg)
	return m, cmd
}

// saveBodyCapture writes the captured body to the note (frontmatter preserved).
func (m *Model) saveBodyCapture() {
	if m.locked {
		m.setStatus("locked (read-only) — ctrl+l to unlock")
		return
	}
	body := strings.TrimRight(m.bodyArea.Value(), "\n")
	if body == "" {
		m.setStatus("note added (title only)")
		return
	}
	for _, n := range m.notes {
		if n.ID == m.bodyNoteID {
			n.Body = body
			n.Updated = time.Now().Format(time.RFC3339)
			if err := n.Save(); err != nil {
				m.setStatus("save failed: " + err.Error())
				return
			}
			break
		}
	}
	m.reload()
	m.setStatus("note saved")
}

// bodyEditView renders the full-screen body-capture editor.
func (m *Model) bodyEditView() string {
	head := stSec.Render("  ✎ new note body") + stDim.Render("   "+m.status)
	hint := stKeyBg.Render(" Ctrl+S ") + stBarBg.Render(" save  ") +
		stKeyBg.Render(" Esc ") + stBarBg.Render(" keep title-only ")
	footer := hint + barPad(m.width-lipgloss.Width(hint))
	return head + "\n\n" + m.bodyArea.View() + "\n" + footer
}
