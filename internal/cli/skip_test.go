package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdSkip(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "standup", "--recur", "weekly", "--due", "today")

	var rows []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 task, got %d", len(rows))
	}
	before := rows[0].Due
	id := rows[0].ID

	out := captureRun(t, "skip", id)
	if !strings.Contains(out, "skipped") {
		t.Fatalf("skip output: %q", out)
	}

	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	// still one task (no completion/spawn), but due advanced a week.
	if len(rows) != 1 {
		t.Fatalf("skip should not spawn/complete; got %d tasks", len(rows))
	}
	if rows[0].Due <= before {
		t.Fatalf("skip should advance the due date: before=%q after=%q", before, rows[0].Due)
	}

	// Skipping a non-recurring task is refused (non-zero exit).
	captureRun(t, "add", "one-off")
	_ = json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows)
	var plainID string
	for _, r := range rows {
		if r.Text == "one-off" {
			plainID = r.ID
		}
	}
	_, code := runWithStdout("skip", plainID)
	if code == 0 {
		t.Errorf("skip of a non-recurring task should exit non-zero")
	}
}
