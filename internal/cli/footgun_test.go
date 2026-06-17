package cli

import (
	"encoding/json"
	"strings"
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

// TestRmTaskRequiresYesNonInteractive: a non-interactive `nt rm <task>` refuses
// without --yes (matching the cautious note-with-backlinks behavior), and
// proceeds with --yes.
func TestRmTaskRequiresYesNonInteractive(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "delete me")
	id := taskIDByText(t, "delete me")

	if _, code := runWithStdout("rm", id); code == 0 {
		t.Fatal("rm of a task non-interactively without --yes should fail")
	}
	// Still present.
	if _, code := runWithStdout("rm", id, "--yes"); code != 0 {
		t.Fatalf("rm --yes should succeed, got exit %d", code)
	}
	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json", "--all")), &listed)
	for _, j := range listed {
		if j.ID == id {
			t.Fatal("task should be deleted after rm --yes")
		}
	}
}

// TestRmValidatesAllHandlesUpFront: an invalid handle alongside a valid note
// aborts before the note is trashed (atomic classification).
func TestRmValidatesAllHandlesUpFront(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Keep me", "--folder", "ref", "--body", "x")

	if _, code := runWithStdout("rm", "Keep me", "nonexistent-handle", "--yes"); code == 0 {
		t.Fatal("rm with an unknown handle should fail")
	}
	// The note must NOT have been trashed by the partial run.
	if out := captureRun(t, "notes"); !strings.Contains(out, "Keep me") {
		t.Fatalf("note should survive an aborted multi-handle rm:\n%s", out)
	}
}

// TestStrayFlagRejected: `nt done --json id` treats the flag-looking token as an
// unknown flag rather than a bad handle.
func TestStrayFlagRejected(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "a task")
	id := taskIDByText(t, "a task")
	if _, code := runWithStdout("done", "--json", id); code == 0 {
		t.Fatal("done with a stray --json flag should be rejected")
	}
}
