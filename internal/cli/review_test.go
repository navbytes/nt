package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func reviewTaskID(t *testing.T, substr string) string {
	t.Helper()
	var rows []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if strings.Contains(r.Text, substr) {
			return r.ID
		}
	}
	t.Fatalf("no task containing %q", substr)
	return ""
}

func TestCmdReview(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "pay invoice", "--due", "2026-01-01") // overdue
	captureRun(t, "add", "vague idea")                         // undated

	out := captureRun(t, "review")
	if !strings.Contains(out, "Overdue") || !strings.Contains(out, "pay invoice") {
		t.Errorf("review should list the overdue task:\n%s", out)
	}
	if !strings.Contains(out, "No due date") || !strings.Contains(out, "vague idea") {
		t.Errorf("review should list the undated task:\n%s", out)
	}
}

func TestCmdReviewStuckProject(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "ship feature +launch") // becomes blocked
	captureRun(t, "add", "fix blocker")          // the blocker (no project)

	blocked := reviewTaskID(t, "ship feature")
	blocker := reviewTaskID(t, "fix blocker")
	captureRun(t, "update", blocker, "--blocks", blocked)

	out := captureRun(t, "review")
	// +launch's only open task is blocked → no next action → stuck project.
	if !strings.Contains(out, "Stuck projects") || !strings.Contains(out, "+launch") {
		t.Errorf("review should flag +launch as stuck:\n%s", out)
	}
}
