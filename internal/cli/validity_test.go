package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdNoteValidFromUntilFlags(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Token lifetime", "--valid-from", "2025-01-01", "--valid-until", "2026-01-01")
	b, _ := os.ReadFile(filepath.Join(dir, "notes", "token-lifetime.md"))
	s := string(b)
	if !strings.Contains(s, "valid_from: 2025-01-01") || !strings.Contains(s, "valid_until: 2026-01-01") {
		t.Fatalf("validity flags not written:\n%s", s)
	}
}

func TestCmdRecallFlagsExpiredNote(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Old lesson", "--lesson", "--valid-until", "2000-01-01")
	out := captureRun(t, "recall", "--lessons-only")
	if !strings.Contains(out, "expired") {
		t.Fatalf("expired lesson should be flagged in nt recall:\n%s", out)
	}
}

func TestCmdEditSetsAndClearsValidity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Fact")
	captureRun(t, "edit", "fact", "--valid-until", "2026-01-01")
	b, _ := os.ReadFile(filepath.Join(dir, "notes", "fact.md"))
	if !strings.Contains(string(b), "valid_until: 2026-01-01") {
		t.Fatalf("edit --valid-until didn't set it:\n%s", b)
	}
	captureRun(t, "edit", "fact", "--clear-valid-until")
	b, _ = os.ReadFile(filepath.Join(dir, "notes", "fact.md"))
	if strings.Contains(string(b), "valid_until") {
		t.Fatalf("edit --clear-valid-until didn't remove it:\n%s", b)
	}
}

func TestCmdEditValidFromConflictsWithClear(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Fact")
	if _, code := runWithStdout("edit", "fact", "--valid-from", "2026-01-01", "--clear-valid-from"); code == 0 {
		t.Fatal("edit should refuse --valid-from combined with --clear-valid-from")
	}
}
