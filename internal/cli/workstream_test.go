package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nt add stamps ws: when NT_WORKSTREAM is set, so a human's CLI writes isolate
// the same way an agent's MCP writes do; unset leaves the task on the shared
// backlog.
func TestAddStampsWorkstreamFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	t.Setenv("NT_WORKSTREAM", "feat-x")
	captureRun(t, "add", "scoped task")

	data, err := os.ReadFile(filepath.Join(dir, "tasks.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ws:feat-x") {
		t.Fatalf("add should stamp ws:feat-x when NT_WORKSTREAM is set:\n%s", data)
	}
}

// A workstream with spaces normalizes to dashes on write AND read: the stamp
// survives the space-delimited todo.txt line (no text leak), and a reader under
// the same spacey identity sees the task (both sides fold to "a-b").
func TestWorkstreamWithSpacesRoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	t.Setenv("NT_WORKSTREAM", "a b")
	captureRun(t, "add", "scoped by spacey stream")

	data, err := os.ReadFile(filepath.Join(dir, "tasks.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ws:a-b") {
		t.Fatalf("spacey workstream should stamp normalized ws:a-b:\n%s", data)
	}

	// The creator (still under "a b") sees its own task, with clean text.
	var listed []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Text != "scoped by spacey stream" {
		t.Fatalf(`reader under "a b" must see the clean task, got %+v`, listed)
	}
	if listed[0].Workstream != "a-b" {
		t.Fatalf("task workstream should read back normalized, got %q", listed[0].Workstream)
	}

	// An explicit --workstream "a b" read normalizes through Scope and matches.
	if out := captureRun(t, "list", "--workstream", "a b", "--json"); !strings.Contains(out, "spacey stream") {
		t.Fatalf(`--workstream "a b" should scope to the normalized stamp:\n%s`, out)
	}

	// Another workstream still doesn't see it.
	t.Setenv("NT_WORKSTREAM", "other")
	if out := captureRun(t, "list", "--json"); strings.Contains(out, "spacey stream") {
		t.Fatalf("another workstream must not see the scoped task:\n%s", out)
	}
}

func TestAddNoWorkstreamWhenUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "add", "shared task")

	data, _ := os.ReadFile(filepath.Join(dir, "tasks.txt"))
	if strings.Contains(string(data), "ws:") {
		t.Fatalf("unset NT_WORKSTREAM should leave the task unstamped:\n%s", data)
	}
}
