package tui

import (
	"fmt"
	"sort"

	"github.com/navbytes/nt/internal/view"
)

// Saved smart views (T10): the `:` palette lists every view from
// $NT_DIR/views.json as "view: <name>"; applying one scopes the task list
// through the shared view.Apply — the same code path as `nt view recall` and
// the web's sidebar — so a named view can never filter differently in the TUI.

// loadViews reads the saved views, sorted by name. Missing file → empty.
func (m *Model) loadViews() []struct {
	Name string
	Spec view.Spec
} {
	views, err := view.Load(m.eng.S.Dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(views))
	for n := range views {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]struct {
		Name string
		Spec view.Spec
	}, 0, len(names))
	for _, n := range names {
		out = append(out, struct {
			Name string
			Spec view.Spec
		}{n, views[n]})
	}
	return out
}

// applyView activates a saved view as the list's base scope. Visibility
// follows the spec: a view that includes done (All / Status:done) or blocked
// tasks turns the matching toggle on, so the view shows what it says it shows.
func (m *Model) applyView(name string, spec view.Spec) {
	m.viewName, m.viewSpec = name, spec
	if spec.All || spec.Status == "done" {
		m.showDone = true
	}
	if spec.All || spec.ShowBlocked || spec.Status == "blocked" {
		m.showBlocked = true
	}
	m.tab, m.cursor, m.offset = tabTasks, 0, 0
	m.rebuild()
	m.setStatus(fmt.Sprintf("view %q — %s (esc clears)", name, spec.Summary()))
}

// clearView drops the active view scope.
func (m *Model) clearView() {
	m.viewName = ""
	m.viewSpec = view.Spec{}
	m.rebuild()
	m.setStatus("view cleared")
}
