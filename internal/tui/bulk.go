package tui

import (
	"fmt"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/task"
)

// applyToTargets applies fn to every target task (marked set or current) in one
// undo transaction, then clears marks and reloads. fn reports whether it actually
// mutated the task; the returned count is the number of real changes (so a no-op,
// e.g. adding a duplicate tag, isn't counted).
func (m *Model) applyToTargets(op string, fn func(*task.Task) bool) int {
	ids := m.targets()
	if len(ids) == 0 {
		return 0
	}
	sel := m.selectedID()
	n := 0
	_ = m.eng.Apply(op, func(d *task.Doc, rec *mutate.Recorder) error {
		for _, id := range ids {
			if tk := d.FindByID(id); tk != nil {
				rec.Before(tk)
				if fn(tk) {
					n++
				}
			}
		}
		return nil
	})
	m.marked = map[string]bool{}
	m.reload()
	if sel != "" {
		m.selectByID(sel)
	}
	return n
}

// doBulkDone completes every open target (spawning recurrences) in one undo
// transaction, so a single `u` reverses the whole bulk action.
func (m *Model) doBulkDone(ids []string) {
	n := 0
	_ = m.eng.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, id := range ids {
			if tk := d.FindByID(id); tk != nil && !tk.Done {
				mutate.Complete(d, rec, tk, mutate.Today())
				n++
			}
		}
		return nil
	})
	m.marked = map[string]bool{}
	m.reload()
	m.setStatus(fmt.Sprintf("done %d", n))
}

// bulkDone completes the marked set, confirming first when any target recurs or
// is hidden (the irreversible/surprising cases).
func (m *Model) bulkDone() {
	ids := m.targets()
	rec, hid := m.recurringOrHidden(ids)
	if rec > 0 || hid > 0 {
		prompt := fmt.Sprintf("Done %d tasks", len(ids))
		if rec > 0 {
			prompt += fmt.Sprintf(", %d recurring", rec)
		}
		if hid > 0 {
			prompt += fmt.Sprintf(", %d hidden", hid)
		}
		m.askConfirm(prompt+"?", func() { m.doBulkDone(ids) })
		return
	}
	m.doBulkDone(ids)
}
