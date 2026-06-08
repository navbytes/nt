package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdAgendaWindowsAndDefer(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "overdue bill due:2020-01-01")
	captureRun(t, "add", "ship release due:today")
	captureRun(t, "add", "dentist due:+3d")
	captureRun(t, "add", "far future due:+90d")
	captureRun(t, "add", "deferred task t:+30d due:+1d")

	texts := func(out string) map[string]bool {
		var rows []taskJSON
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		m := map[string]bool{}
		for _, r := range rows {
			m[r.Text] = true
		}
		return m
	}

	// agenda (7-day horizon): overdue + today + within 7 days; not the +90d task;
	// not the deferred (future-start) task.
	ag := texts(captureRun(t, "agenda", "--json"))
	if !ag["overdue bill"] || !ag["ship release"] || !ag["dentist"] {
		t.Errorf("agenda should include overdue/today/+3d: %v", ag)
	}
	if ag["far future"] {
		t.Error("agenda (7d) should exclude a +90d task")
	}
	if ag["deferred task"] {
		t.Error("agenda should exclude a not-yet-started (future t:) task")
	}

	// today (0-day horizon): overdue + due-today only; not +3d.
	td := texts(captureRun(t, "today", "--json"))
	if !td["overdue bill"] || !td["ship release"] {
		t.Errorf("today should include overdue + due-today: %v", td)
	}
	if td["dentist"] {
		t.Error("today should exclude a +3d task")
	}

	// ready hides the deferred (future-start) task.
	rd := texts(captureRun(t, "ready", "--json"))
	if rd["deferred task"] {
		t.Error("ready must hide a future-start (t:) task")
	}

	// Grouped text output has the bucket headers.
	out := captureRun(t, "agenda")
	for _, h := range []string{"Overdue", "Today", "Upcoming"} {
		if !strings.Contains(out, h) {
			t.Errorf("agenda output missing %q header:\n%s", h, out)
		}
	}
}
