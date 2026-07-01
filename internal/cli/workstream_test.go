package cli

import (
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
