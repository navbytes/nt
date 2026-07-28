package cli

import (
	"strings"
	"testing"
)

func TestArchiveNote(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Auth Decision", "--body", "we chose flock over sqlite")

	// Active → shows up in search.
	if out := captureRun(t, "search", "auth"); !strings.Contains(out, "Auth Decision") {
		t.Fatalf("a live note should be searchable: %s", out)
	}

	// Archive it by handle → drops out of the active search.
	if out := captureRun(t, "archive", "Auth Decision"); !strings.Contains(out, "archived 1 note") {
		t.Errorf("archive output: %s", out)
	}
	if out := captureRun(t, "search", "auth"); strings.Contains(out, "Auth Decision") {
		t.Errorf("an archived note should drop out of search: %s", out)
	}

	// --undo restores it.
	if out := captureRun(t, "archive", "Auth Decision", "--undo"); !strings.Contains(out, "unarchived 1 note") {
		t.Errorf("unarchive output: %s", out)
	}
	if out := captureRun(t, "search", "auth"); !strings.Contains(out, "Auth Decision") {
		t.Errorf("an unarchived note should be searchable again: %s", out)
	}
}

// `nt archive` with no note handle still archives done tasks (no regression).
func TestArchiveTasksStillWorks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "finish it")
	captureRun(t, "done", reviewTaskID(t, "finish it"))
	if out := captureRun(t, "archive"); !strings.Contains(out, "archived 1 task") {
		t.Errorf("no-arg archive should still move done tasks: %s", out)
	}
}

// TestArchiveRejectsBadHandleBeforeWriting is the regression guard for a
// partial-application bug: archiveNotes used to resolve-and-save inside one
// loop, so a trailing bad handle returned an error AFTER the earlier notes had
// already been archived. The command reported failure while the store had
// quietly lost entries from index/search/recall.
func TestArchiveRejectsBadHandleBeforeWriting(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Alpha Note", "--body", "first")
	captureRun(t, "note", "Bravo Note", "--body", "second")

	// A valid handle followed by a typo must archive NOTHING. The command is
	// expected to fail, so use runWithStdout rather than captureRun.
	if _, code := runWithStdout("archive", "Alpha Note", "Bravo Note", "no-such-note"); code == 0 {
		t.Fatal("archive with an unresolvable handle should exit non-zero")
	}

	for _, title := range []string{"Alpha Note", "Bravo Note"} {
		if out := captureRun(t, "search", title); !strings.Contains(out, title) {
			t.Errorf("a failed archive must not archive earlier handles; %q is gone:\n%s", title, out)
		}
	}
}
