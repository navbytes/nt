package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navbytes/nt/internal/task"
)

// tokenSpan is the column range [start,end) of an actionable token within a row.
type tokenSpan struct {
	start, end int
	ft         followTarget
}

// hitLine maps one rendered list line to its selectable item (-1 for headers /
// blanks) and any clickable token spans. Built during list rendering so a later
// click can be resolved to an item or token.
type hitLine struct {
	item   int
	tokens []tokenSpan
}

// tokenOf classifies a word as a link/tag/project token.
func tokenOf(w string) (followTarget, bool) {
	switch {
	case strings.HasPrefix(w, "@") && len(w) > 1:
		return followTarget{kind: "tag", value: w[1:]}, true
	case strings.HasPrefix(w, "+") && len(w) > 1:
		return followTarget{kind: "project", value: w[1:]}, true
	case strings.HasPrefix(w, "[[") && strings.HasSuffix(w, "]]") && len(w) > 4:
		return followTarget{kind: "link", value: w[2 : len(w)-2]}, true
	}
	return followTarget{}, false
}

// taskTokenSpans computes the column ranges of a task row's tokens, where the
// description text begins at startCol (marker+icon+space = 5 in the list).
func taskTokenSpans(t *task.Task, startCol int) []tokenSpan {
	var spans []tokenSpan
	col := startCol
	for _, w := range strings.Fields(t.Text) {
		wl := lipgloss.Width(w)
		if ft, ok := tokenOf(w); ok {
			spans = append(spans, tokenSpan{start: col, end: col + wl, ft: ft})
		}
		col += wl + 1 // word + separating space
	}
	return spans
}

// handleMouse routes a mouse event: wheel scrolls, left-click selects a row or
// activates a clicked token.
func (m *Model) handleMouse(msg tea.MouseMsg) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.detailFocus {
			if m.detailScroll > 0 {
				m.detailScroll--
			}
		} else {
			m.move(-1)
		}
	case tea.MouseButtonWheelDown:
		if m.detailFocus {
			m.detailScroll++
		} else {
			m.move(1)
		}
	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			m.click(msg.X, msg.Y)
		}
	}
}

// click resolves a left-click at (x,y) to a row selection and, if it landed on
// a token, activates it (scope/navigate). In wide mode, clicking the right pane
// focuses the detail for scrolling.
func (m *Model) click(x, y int) {
	headerH := lipgloss.Height(m.headerView())
	footerH := lipgloss.Height(m.footerView())
	bodyH := m.height - headerH - footerH
	row := y - headerH
	if row < 0 || row >= bodyH {
		return // header or footer
	}
	if m.width >= wideMin {
		leftW := m.width * 58 / 100
		if x >= leftW {
			m.detailFocus, m.detailScroll = true, 0 // focus the detail pane
			return
		}
	}
	full := m.offset + row
	if full < 0 || full >= len(m.hitLines) {
		return
	}
	hl := m.hitLines[full]
	if hl.item < 0 {
		return // group header or blank line
	}
	m.cursor = hl.item
	m.detailScroll = 0
	for _, sp := range hl.tokens {
		if x >= sp.start && x < sp.end {
			m.activateTarget(sp.ft, false)
			return
		}
	}
}
