package view

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/task"
)

func parseTasks(t *testing.T, lines ...string) []*task.Task {
	t.Helper()
	d := task.Parse([]byte(strings.Join(lines, "\n")))
	ts := d.Tasks()
	for _, tk := range ts {
		tk.EnsureID()
	}
	return ts
}

func texts(ts []*task.Task) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Text
	}
	return out
}

func TestApplyDefaultHidesDone(t *testing.T) {
	ts := parseTasks(t, "open one", "x 2026-01-01 finished", "open two")
	got := Apply(ts, Spec{}, nil)
	if len(got) != 2 {
		t.Fatalf("default view should hide done, got %v", texts(got))
	}
	all := Apply(ts, Spec{All: true}, nil)
	if len(all) != 3 {
		t.Errorf("All should include done, got %v", texts(all))
	}
}

func TestApplyStatusMatchesEffective(t *testing.T) {
	ts := parseTasks(t, "free task", "gated task")
	blocked := map[string]bool{ts[1].ID(): true}
	got := Apply(ts, Spec{Status: "blocked"}, blocked)
	if len(got) != 1 || got[0].Text != "gated task" {
		t.Errorf("Status:blocked should match the dependency-blocked task, got %v", texts(got))
	}
	open := Apply(ts, Spec{Status: "open"}, blocked)
	if len(open) != 1 || open[0].Text != "free task" {
		t.Errorf("Status:open should exclude the blocked one, got %v", texts(open))
	}
}

func TestApplyTagAndProject(t *testing.T) {
	ts := parseTasks(t, "a @home", "b +api @work", "c +api")
	if got := Apply(ts, Spec{Tag: "work"}, nil); len(got) != 1 || got[0].Text != "b +api @work" {
		t.Errorf("Tag filter, got %v", texts(got))
	}
	if got := Apply(ts, Spec{Project: "api"}, nil); len(got) != 2 {
		t.Errorf("Project filter, got %v", texts(got))
	}
}

func TestApplySortDue(t *testing.T) {
	ts := parseTasks(t, "later due:2026-09-01", "none", "soon due:2026-06-01")
	got := Apply(ts, Spec{Sort: "due"}, nil)
	// Text is the description only — the due: kv is parsed out of it.
	want := []string{"soon", "later", "none"}
	for i, w := range want {
		if got[i].Text != w {
			t.Fatalf("due sort order = %v, want %v", texts(got), want)
		}
	}
}
