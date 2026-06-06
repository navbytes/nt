package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// captureRun runs a CLI invocation against a fresh store, returning stdout.
func captureRun(t *testing.T, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	code := Run(args)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if code != 0 {
		t.Fatalf("nt %s exited %d: %s", strings.Join(args, " "), code, out)
	}
	return string(out)
}

func TestCmdLog(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "ship release", "--source", "claude")
	captureRun(t, "add", "write docs")
	captureRun(t, "add", "still open")

	// Complete the two tasks by their short ids (parsed from `add` output is
	// noisy; resolve via list --json instead).
	var listed []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	for _, j := range listed {
		if j.Text == "ship release" || j.Text == "write docs" {
			captureRun(t, "done", j.ID)
		}
	}

	// Plain log lists both completed tasks, not the open one.
	out := captureRun(t, "log")
	if !strings.Contains(out, "ship release") || !strings.Contains(out, "write docs") {
		t.Fatalf("log should list both completed tasks:\n%s", out)
	}
	if strings.Contains(out, "still open") {
		t.Fatalf("log must not include open tasks:\n%s", out)
	}

	// --source filters; --json carries the completion date.
	var logged []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "log", "--json", "--source", "claude")), &logged); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 || logged[0].Text != "ship release" {
		t.Fatalf("--source claude should yield only the claude task, got %+v", logged)
	}
	if logged[0].Completed == "" {
		t.Fatal("log --json should include the completion date")
	}
}
