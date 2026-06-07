package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDiscoveredFrom: --discovered-from records a provenance edge that nt links
// surfaces in both directions.
func TestDiscoveredFrom(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "refactor auth middleware", "--source", "claude")

	var listed []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	origin := listed[0].ID

	captureRun(t, "add", "backfill user.tier column", "--discovered-from", origin, "--source", "claude")

	// Origin's links show what was discovered from it.
	if out := captureRun(t, "links", origin); !strings.Contains(out, "discovered here") ||
		!strings.Contains(out, "backfill user.tier column") {
		t.Fatalf("origin links should show 'discovered here' + the child:\n%s", out)
	}

	// The child's links show where it came from.
	var all []taskJSON
	_ = json.Unmarshal([]byte(captureRun(t, "list", "--json")), &all)
	var child string
	for _, j := range all {
		if j.Text == "backfill user.tier column" {
			child = j.ID
		}
	}
	if child == "" {
		t.Fatal("child task not found")
	}
	if out := captureRun(t, "links", child); !strings.Contains(out, "discovered from") ||
		!strings.Contains(out, "refactor auth middleware") {
		t.Fatalf("child links should show 'discovered from' + the origin:\n%s", out)
	}
}
