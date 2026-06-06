package tui

import (
	"fmt"

	"github.com/navbytes/nt/internal/mutate"
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
	prompt := "Delete this task?"
	if len(ids) > 1 {
		prompt = fmt.Sprintf("Delete %d tasks?", len(ids))
	}
	if h := m.hiddenMarked(); h > 0 {
		prompt = fmt.Sprintf("Delete %d tasks (%d hidden)?", len(ids), h)
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
