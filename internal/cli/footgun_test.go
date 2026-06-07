package cli

import (
	"encoding/json"
	"testing"
)

// TestPositionalHandleGatedForAgents: positional task:N / bare N is refused for
// non-interactive callers (stdout piped here), while the stable ULID still works
// — so an agent can't act on a shifted index.
func TestPositionalHandleGatedForAgents(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "first task")
	captureRun(t, "add", "second task")

	var listed []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	id := listed[0].ID

	for _, h := range []string{"task:1", "1"} {
		if _, code := runWithStdout("done", h); code == 0 {
			t.Errorf("done %q should be rejected for a non-interactive caller (got exit 0)", h)
		}
	}

	// The same is enforced for the other mutating/resolve commands.
	for _, cmd := range [][]string{{"rm", "task:1"}, {"update", "1", "--pri", "high"}, {"edit", "task:2"}} {
		if _, code := runWithStdout(cmd...); code == 0 {
			t.Errorf("%v should be rejected for a non-interactive caller", cmd)
		}
	}

	// Stable id works non-interactively.
	if _, code := runWithStdout("done", id); code != 0 {
		t.Errorf("done by stable id should work for an agent, got exit %d", code)
	}
}
