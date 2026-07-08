package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCmdEditExpectMtimeGuardsAgainstConcurrentWrite(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "mtime guard", "--body", "original")

	var one noteJSON
	out := captureRun(t, "show", "mtime guard", "--json")
	if err := json.Unmarshal([]byte(out), &one); err != nil {
		t.Fatalf("unmarshal show --json: %v\n%s", err, out)
	}
	if one.MTime == "" {
		t.Fatal("nt show --json should return a non-empty mtime")
	}

	// Simulate a concurrent writer touching the file after this fetch.
	time.Sleep(10 * time.Millisecond)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(one.Path, future, future); err != nil {
		t.Fatal(err)
	}

	_, code := runWithStdout("edit", "mtime guard", "--append", "clobber", "--expect-mtime", one.MTime)
	if code == 0 {
		t.Fatal("expected `nt edit --expect-mtime` to refuse with a stale token")
	}

	after := captureRun(t, "show", "mtime guard", "--json")
	if strings.Contains(after, "clobber") {
		t.Error("nt edit wrote despite refusing — the file was clobbered")
	}
}

func TestCmdEditWithoutExpectMtimeStillWorks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "no token", "--body", "original")
	captureRun(t, "edit", "no token", "--append", "more")
	after := captureRun(t, "show", "no token", "--json")
	if !strings.Contains(after, "more") {
		t.Errorf("edit without --expect-mtime should still apply: %s", after)
	}
}
