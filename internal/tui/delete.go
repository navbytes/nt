package tui

import (
	"fmt"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// deleteTargets asks to delete the marked set (or current task), then deletes on
// confirm. Delete is journaled, so `u` undoes it — but it still confirms, since
// removal is a destructive-feeling action.
func (m *Model) deleteTargets() {
	ids := m.targets()
	if len(ids) == 0 {
		return
	}
	prompt := "delete this task? (u undoes)"
	if len(ids) > 1 {
		prompt = fmt.Sprintf("delete %d tasks? (u undoes)", len(ids))
	}
	if h := m.hiddenMarked(); h > 0 {
		prompt = fmt.Sprintf("delete %d tasks (%d hidden)? (u undoes)", len(ids), h)
	}
	m.askConfirm(prompt, func() { m.doDelete(ids) })
}

// doDelete removes the given tasks in one undo transaction (before-images are
// journaled, so undo re-adds them).
func (m *Model) doDelete(ids []string) {
	n := 0
	_ = m.eng.Apply("delete", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, id := range ids {
			if before, ok := d.Remove(id); ok {
				rec.Removed(id, before)
				n++
			}
		}
		return nil
	})
	m.marked = map[string]bool{}
	m.reload()
	m.setStatus(fmt.Sprintf("deleted %d (u to undo)", n))
}

// deleteNoteTarget deletes the selected note to .trash/. If other items link to
// it, it surfaces the inbound-link count and offers — like the CLI — to strip
// those links first (unlink) or delete anyway and leave them dangling (force),
// so the graph isn't silently broken. (`x` archives instead, reversibly.)
func (m *Model) deleteNoteTarget() {
	n := m.selectedNote()
	if n == nil {
		return
	}
	back := links.Backlinks(m.eng.S, n.ID, n.Rel)
	if len(back) == 0 {
		m.askConfirm(fmt.Sprintf("delete note %q? (→ .trash, u undoes)", n.Title), func() { m.doDeleteNote(n, false) })
		return
	}
	prompt := fmt.Sprintf("%q has %d inbound link(s) — u: unlink & delete · f: delete anyway · esc: cancel", n.Title, len(back))
	m.askChoice(prompt, "(u/f/esc)", map[string]func(){
		"u": func() { m.doDeleteNote(n, true) },
		"f": func() { m.doDeleteNote(n, false) },
	})
}

// doDeleteNote optionally strips inbound links, then moves the note to .trash/.
func (m *Model) doDeleteNote(n *note.Note, unlink bool) {
	stripped := 0
	if unlink {
		updated, err := m.eng.UnlinkNote(n)
		if err != nil {
			m.setStatus("delete failed: " + err.Error())
			return
		}
		stripped = updated
	}
	if err := m.eng.TrashNote(n); err != nil {
		m.setStatus("delete failed: " + err.Error())
		return
	}
	m.reload()
	if stripped > 0 {
		m.setStatus(fmt.Sprintf("deleted note → .trash/ (unlinked %d reference(s))", stripped))
	} else {
		m.setStatus("deleted note → .trash/")
	}
}
