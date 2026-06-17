package tui

import "fmt"

// confirmState is a pending confirmation for a destructive action. In binary
// mode (action set) the next y/enter runs action; in multi-choice mode (choices
// set) the next key selects a handler, anything unmatched cancels.
type confirmState struct {
	prompt  string
	action  func()
	choices map[string]func()
	hint    string // key hint rendered in the status bar (defaults to "(y/n/⏎)")
}

// askConfirm arms a y/n confirmation; the next y/enter runs action, anything
// else cancels (handled in Update before normal key dispatch).
func (m *Model) askConfirm(prompt string, action func()) {
	m.confirm = &confirmState{prompt: prompt, action: action}
}

// askChoice arms a multi-key confirmation (e.g. delete-with-backlinks): each key
// in choices runs its handler; any other key cancels. hint is the status-bar key
// legend, e.g. "(u/f/esc)".
func (m *Model) askChoice(prompt, hint string, choices map[string]func()) {
	m.confirm = &confirmState{prompt: prompt, choices: choices, hint: hint}
}

// toggleMark marks/unmarks the current task.
func (m *Model) toggleMark() {
	t := m.selectedTask()
	if t == nil {
		return
	}
	if m.marked[t.ID()] {
		delete(m.marked, t.ID())
	} else {
		m.marked[t.ID()] = true
	}
	m.markStatus()
}

// startVisual toggles V range-select. Entering anchors at the cursor; while
// active, moving paints the crossed rows into the marked set.
func (m *Model) startVisual() {
	if m.tab != tabTasks {
		return
	}
	if m.visualMode {
		m.visualMode = false
		m.markStatus()
		return
	}
	m.visualMode = true
	m.visualAnchor = m.cursor
	if t := m.selectedTask(); t != nil {
		m.marked[t.ID()] = true
	}
	m.markStatus()
}

// paintVisual marks every task between the anchor and the cursor (additive).
func (m *Model) paintVisual() {
	if !m.visualMode {
		return
	}
	lo, hi := m.visualAnchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	for i := lo; i <= hi && i < len(m.flat); i++ {
		m.marked[m.flat[i].ID()] = true
	}
	m.markStatus()
}

// clearMarks drops all marks and exits visual mode; reports whether anything
// was cleared (so esc can fall through to filter/scope when there was nothing).
func (m *Model) clearMarks() bool {
	if len(m.marked) == 0 && !m.visualMode {
		return false
	}
	m.marked = map[string]bool{}
	m.visualMode = false
	m.setStatus("marks cleared")
	return true
}

// hiddenMarked counts marked tasks not currently in the visible list (filtered
// or scoped out) — surfaced so a bulk op can't silently hit invisible tasks.
func (m *Model) hiddenMarked() int {
	inView := make(map[string]bool, len(m.flat))
	for _, t := range m.flat {
		inView[t.ID()] = true
	}
	n := 0
	for id := range m.marked {
		if !inView[id] {
			n++
		}
	}
	return n
}

func (m *Model) markStatus() {
	if len(m.marked) == 0 {
		m.status = ""
		return
	}
	s := fmt.Sprintf("%d marked", len(m.marked))
	if h := m.hiddenMarked(); h > 0 {
		s += fmt.Sprintf(" (%d hidden)", h)
	}
	if m.visualMode {
		s = "visual · " + s
	}
	m.setStatus(s)
}

// targets returns the ULIDs a mutating action should act on: the marked set
// (visible first, then hidden marks) if any, else the current task.
func (m *Model) targets() []string {
	if len(m.marked) > 0 {
		seen := make(map[string]bool, len(m.marked))
		var ids []string
		for _, t := range m.flat {
			if m.marked[t.ID()] && !seen[t.ID()] {
				ids = append(ids, t.ID())
				seen[t.ID()] = true
			}
		}
		for id := range m.marked {
			if !seen[id] {
				ids = append(ids, id)
			}
		}
		return ids
	}
	if t := m.selectedTask(); t != nil {
		return []string{t.ID()}
	}
	return nil
}

// recurringOrHidden reports whether any target task recurs or is hidden — the
// conditions under which a bulk `done` should ask for confirmation.
func (m *Model) recurringOrHidden(ids []string) (recurring, hidden int) {
	inView := make(map[string]bool, len(m.flat))
	byID := make(map[string]bool, len(m.flat))
	for _, t := range m.flat {
		inView[t.ID()] = true
		byID[t.ID()] = true
	}
	for _, id := range ids {
		if !inView[id] {
			hidden++
		}
		for _, t := range m.tasks {
			if t.ID() == id && t.Recur() != "" {
				recurring++
			}
		}
	}
	return recurring, hidden
}
