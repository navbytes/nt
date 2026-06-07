package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdNoteFolderFlag: `nt note … --folder work/auth` files the note in that
// subfolder.
func TestCmdNoteFolderFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	out := captureRun(t, "note", "Auth Design", "--folder", "work/auth", "--body", "scoped")
	if !strings.Contains(out, "notes/work/auth/auth-design.md") {
		t.Fatalf("unexpected output: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "work", "auth", "auth-design.md")); err != nil {
		t.Fatalf("note not created in subfolder: %v", err)
	}
}

// TestCmdNotePathStyle: `nt note "work/Token rotation"` treats the prefix as the
// folder when no --folder flag is given.
func TestCmdNotePathStyle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	out := captureRun(t, "note", "work/Token rotation")
	if !strings.Contains(out, "notes/work/token-rotation.md") {
		t.Fatalf("path-style folder not applied: %q", out)
	}
}

// TestCmdNoteRootStillWorks: a plain title with no slash lands in notes/ root.
func TestCmdNoteRootStillWorks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	out := captureRun(t, "note", "Just a root note")
	if !strings.Contains(out, "notes/just-a-root-note.md") {
		t.Fatalf("root note path wrong: %q", out)
	}
}
