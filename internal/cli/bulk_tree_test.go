package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBulkUpdate(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "task one")
	captureRun(t, "add", "task two")
	captureRun(t, "add", "task three")

	var rows []taskJSON
	_ = json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows)
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}

	// Update all three at once to priority D and status doing.
	out := captureRun(t, "update", ids[0], ids[1], ids[2], "--pri", "D", "--status", "doing")
	if !strings.Contains(out, "updated 3") {
		t.Fatalf("bulk update output: %q", out)
	}
	_ = json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows)
	for _, r := range rows {
		if r.Status != "doing" {
			t.Errorf("task %q status = %q, want doing", r.Text, r.Status)
		}
	}
}

func TestListTree(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "epic")
	var rows []taskJSON
	_ = json.Unmarshal([]byte(captureRun(t, "list", "--json")), &rows)
	parent := rows[0].ID
	captureRun(t, "add", "child a", "--parent", parent)
	captureRun(t, "add", "child b", "--parent", parent)

	out := captureRun(t, "list", "--tree")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// The epic line shows child progress (0/2) and children are indented under it.
	var epicLine string
	for _, l := range lines {
		if strings.Contains(l, "epic") {
			epicLine = l
		}
	}
	if !strings.Contains(epicLine, "(0/2)") {
		t.Errorf("epic should show child progress (0/2): %q", epicLine)
	}
	childIndented := false
	for _, l := range lines {
		if strings.Contains(l, "child a") && strings.HasPrefix(l, "  ") {
			childIndented = true
		}
	}
	if !childIndented {
		t.Errorf("children should be indented under the parent:\n%s", out)
	}
}
