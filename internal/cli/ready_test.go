package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdReady(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "urgent thing", "--pri", "high", "--due", "today", "--source", "claude")
	captureRun(t, "add", "someday thing")
	captureRun(t, "add", "blocker task")
	captureRun(t, "add", "done thing")

	var listed []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	id := func(text string) string {
		for _, j := range listed {
			if j.Text == text {
				return j.ID
			}
		}
		t.Fatalf("no task %q", text)
		return ""
	}
	// "blocker task" blocks "someday thing" (so someday is blocked); "done thing"
	// is completed. (A --blocks B ⇒ B is the one that becomes blocked.)
	captureRun(t, "update", id("blocker task"), "--blocks", id("someday thing"))
	captureRun(t, "done", id("done thing"))

	var ready []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "ready", "--json")), &ready); err != nil {
		t.Fatal(err)
	}
	texts := make(map[string]bool)
	for _, j := range ready {
		texts[j.Text] = true
	}
	if texts["done thing"] {
		t.Error("ready must exclude completed tasks")
	}
	if texts["someday thing"] {
		t.Error("ready must exclude dependency-blocked tasks")
	}
	if !texts["urgent thing"] || !texts["blocker task"] {
		t.Errorf("ready should include open, unblocked tasks; got %v", texts)
	}
	// Urgency order: the high-priority due-today task comes first.
	if len(ready) == 0 || ready[0].Text != "urgent thing" {
		t.Errorf("ready should be urgency-sorted (urgent thing first), got %+v", ready)
	}

	// --source narrows to that origin.
	out := captureRun(t, "ready", "--source", "claude")
	if !strings.Contains(out, "urgent thing") || strings.Contains(out, "blocker task") {
		t.Errorf("--source claude should keep only the claude task:\n%s", out)
	}
}
