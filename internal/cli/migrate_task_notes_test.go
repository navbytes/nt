package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The one-off migration moves auto task-detail notes out of the legacy
// notes/tasks/ folder into notes/__tasks__/, but only the ones a task links to —
// a user's own note that merely lives in tasks/ is left untouched.
func TestMigrateTaskNotes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)

	// A task-detail note in the legacy folder, linked from a task by title.
	captureRun(t, "note", "Legacy Detail", "--folder", "tasks", "--body", "old reasoning")
	captureRun(t, "add", "do the thing [[Legacy Detail]]")
	// A hand-written note that just happens to live in tasks/ — not linked.
	captureRun(t, "note", "My Own Note", "--folder", "tasks", "--body", "mine, keep here")

	notesDir := filepath.Join(dir, "notes")
	legacyLinked := filepath.Join(notesDir, "tasks", "legacy-detail.md")
	legacyOwn := filepath.Join(notesDir, "tasks", "my-own-note.md")
	movedLinked := filepath.Join(notesDir, "__tasks__", "legacy-detail.md")

	if !exists(legacyLinked) || !exists(legacyOwn) {
		t.Fatalf("setup: expected both notes under notes/tasks/")
	}

	// Dry-run names the linked note and does NOT move anything.
	out := captureRun(t, "migrate-task-notes")
	if !strings.Contains(out, "would move 1") || !strings.Contains(out, "legacy-detail") {
		t.Fatalf("dry-run should preview the linked note: %s", out)
	}
	if exists(movedLinked) {
		t.Fatalf("dry-run must not move files")
	}

	// Apply moves only the linked note; the hand-written one stays put.
	out = captureRun(t, "migrate-task-notes", "--apply")
	if !strings.Contains(out, "moved 1") {
		t.Fatalf("apply output: %s", out)
	}
	if !exists(movedLinked) {
		t.Errorf("linked task note should have moved to notes/__tasks__/")
	}
	if exists(legacyLinked) {
		t.Errorf("linked task note should no longer be under notes/tasks/")
	}
	if !exists(legacyOwn) {
		t.Errorf("a user's own note in tasks/ must be left untouched")
	}

	// The moved note is still a live, searchable note (resolution is by
	// path-suffix/title, so the task's [[Legacy Detail]] link keeps working).
	if out := captureRun(t, "search", "reasoning"); !strings.Contains(out, "Legacy Detail") {
		t.Errorf("moved note should still be searchable: %s", out)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
