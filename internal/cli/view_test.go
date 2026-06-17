package cli

import (
	"strings"
	"testing"
)

func TestViewSaveListRecallRemove(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "add", "ship it", "--project", "work", "--pri", "high")
	captureRun(t, "add", "blog post", "--project", "personal")

	if out := captureRun(t, "view", "list"); !strings.Contains(out, "no saved views") {
		t.Errorf("fresh store should have no views, got: %s", out)
	}

	if out := captureRun(t, "view", "save", "work", "--project", "work", "--sort", "urgency"); !strings.Contains(out, "saved view") {
		t.Errorf("save output: %s", out)
	}
	if out := captureRun(t, "view", "list"); !strings.Contains(out, "work") || !strings.Contains(out, "--project work") {
		t.Errorf("view list should show the saved view + its flags, got: %s", out)
	}

	// Recall (shorthand) must apply the saved filter: only the work task.
	out := captureRun(t, "view", "work")
	if !strings.Contains(out, "ship it") || strings.Contains(out, "blog post") {
		t.Errorf("recall should filter to project=work:\n%s", out)
	}
	// recall == explicit form, and --json is honored at recall time.
	if j := captureRun(t, "view", "recall", "work", "--json"); !strings.Contains(j, "\"project\": \"work\"") {
		t.Errorf("recall --json should emit machine output, got: %s", j)
	}

	// Re-saving updates in place (no duplicate).
	if out := captureRun(t, "view", "save", "work", "--project", "work"); !strings.Contains(out, "updated view") {
		t.Errorf("re-save should report Updated, got: %s", out)
	}

	// Reserved names and unknown recalls are errors (non-zero exit).
	if _, code := runWithStdout("view", "save", "list", "--project", "x"); code == 0 {
		t.Error("saving a reserved name should fail")
	}
	if _, code := runWithStdout("view", "nope"); code == 0 {
		t.Error("recalling an unknown view should fail")
	}

	captureRun(t, "view", "rm", "work")
	if out := captureRun(t, "view", "list"); !strings.Contains(out, "no saved views") {
		t.Errorf("view should be gone after rm, got: %s", out)
	}
	if _, code := runWithStdout("view", "rm", "work"); code == 0 {
		t.Error("removing a missing view should fail")
	}
}
