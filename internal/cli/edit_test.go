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

// TestEditTitleRepairsAMangledTitle covers the gap that made a wrong title
// permanent from the CLI: `nt edit` had --desc but no --title, and `nt mv`
// pins the OLD title into frontmatter where it beats the body H1 — so there
// was no non-interactive way to fix one. This is also the recovery path for a
// title the path-style shorthand truncated.
func TestEditTitleRepairsAMangledTitle(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "cli test-gap audit", "--body", "# cli test-gap audit\n\nfindings here")

	out := captureRun(t, "edit", "cli test-gap audit", "--title", "internal/cli test-gap audit")
	if !strings.Contains(out, "retitled") && !strings.Contains(out, "edited") {
		t.Errorf("edit --title should report what it did: %s", out)
	}

	shown := captureRun(t, "show", "internal/cli test-gap audit")
	if !strings.Contains(shown, "internal/cli test-gap audit") {
		t.Errorf("the new title should be resolvable and displayed:\n%s", shown)
	}
	// The stale H1 must not survive to contradict the frontmatter.
	if strings.Contains(shown, "# cli test-gap audit\n") {
		t.Errorf("the old H1 should have been rewritten with the title:\n%s", shown)
	}
	if !strings.Contains(shown, "findings here") {
		t.Errorf("retitling must not disturb the body:\n%s", shown)
	}
}
