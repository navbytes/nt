package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// runWithStderr runs a CLI invocation with stdout AND stderr piped, returning
// both streams and the exit code — for asserting on usage/teaching errors.
func runWithStderr(args ...string) (stdout, stderr string, code int) {
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	code = Run(args)
	wo.Close()
	we.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	so, _ := io.ReadAll(ro)
	se, _ := io.ReadAll(re)
	return string(so), string(se), code
}

// --kind memory files under memory/ with the memory-core tag (the always-loaded
// core-memory layer) — NOT a bare "memory" tag.
func TestNoteKindMemory(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "user prefers tabs over spaces", "--kind", "memory")
	var j struct {
		Path string   `json:"path"`
		Tags []string `json:"tags"`
	}
	json.Unmarshal([]byte(captureRun(t, "show", "user prefers tabs over spaces", "--json")), &j)
	if !strings.Contains(j.Path, "memory/") {
		t.Fatalf("--kind memory should file under memory/, got %s", j.Path)
	}
	if !contains(j.Tags, "memory-core") {
		t.Fatalf("--kind memory should tag 'memory-core', got %v", j.Tags)
	}
	if contains(j.Tags, "memory") {
		t.Fatalf("--kind memory must NOT add a bare 'memory' tag, got %v", j.Tags)
	}
}

// `--blocked-by none` is a natural but wrong guess (the edge lives on the
// blocker) — it must teach the real clearing move, not fail with "no task none".
func TestUpdateBlockedByNoneTeaches(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "waiting task")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)

	_, stderr, code := runWithStderr("update", listed[0].ID, "--blocked-by", "none")
	if code == 0 {
		t.Fatal("update --blocked-by none should be a usage error")
	}
	if !strings.Contains(stderr, "--blocked-by takes a task id") ||
		!strings.Contains(stderr, "--blocks none") {
		t.Fatalf("expected the teaching error pointing at --blocks none, got:\n%s", stderr)
	}

	// The add path teaches the same way.
	_, stderr, code = runWithStderr("add", "new task", "--blocked-by", "none")
	if code == 0 || !strings.Contains(stderr, "--blocked-by takes a task id") {
		t.Fatalf("add --blocked-by none should teach too, got code %d:\n%s", code, stderr)
	}
}
