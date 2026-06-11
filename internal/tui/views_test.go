package tui

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/view"
)

// TestApplySavedView: applying a saved view scopes the task list through the
// shared view.Apply (same as the CLI/web), shows a header chip, lists in the
// palette, and Esc clears it.
func TestApplySavedView(t *testing.T) {
	m := testModel(t) // seeds: "fix auth bug @backend +api due:today", "write tests +api", "deploy"
	if err := view.Save(m.eng.S.Dir, map[string]view.Spec{"backend": {Tag: "backend"}}); err != nil {
		t.Fatal(err)
	}

	// The palette offers the saved view.
	found := false
	for _, c := range m.viewPaletteCommands() {
		if c.name == "view: backend" {
			found = true
			c.run(m)
		}
	}
	if !found {
		t.Fatal("palette should list the saved view")
	}

	// The list is scoped to @backend tasks only.
	if got := len(m.scopedTasks()); got != 1 {
		t.Fatalf("view scope should keep 1 @backend task, got %d", got)
	}
	if m.viewName != "backend" {
		t.Fatalf("viewName = %q", m.viewName)
	}

	// A "clear view" entry appears while active.
	hasClear := false
	for _, c := range m.viewPaletteCommands() {
		if c.name == "clear view" {
			hasClear = true
		}
	}
	if !hasClear {
		t.Error("palette should offer 'clear view' while a view is active")
	}

	// The header shows the view chip.
	m.width, m.height = 100, 30
	if !strings.Contains(m.View(), "⊞ backend") {
		t.Error("header should show the active view chip")
	}

	// Esc clears the view scope.
	if !m.backOut() {
		t.Fatal("esc should be consumed to clear the view")
	}
	if m.viewName != "" || len(m.scopedTasks()) != 3 {
		t.Errorf("esc should clear the view: name=%q tasks=%d", m.viewName, len(m.scopedTasks()))
	}
}

// TestViewVisibilityFollowsSpec: a view that includes done tasks (--all) turns
// the done toggle on so the view shows what it says it shows.
func TestViewVisibilityFollowsSpec(t *testing.T) {
	m := testModel(t)
	m.showDone = false
	m.applyView("everything", view.Spec{All: true})
	if !m.showDone {
		t.Error("a view with All should enable showDone")
	}
	if !m.showBlocked {
		t.Error("a view with All should enable showBlocked")
	}
}
