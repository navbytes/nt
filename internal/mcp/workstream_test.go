package mcp

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDeriveWorkstreamNonGit: outside a git repo, "auto" falls back to the
// working-directory basename.
func TestDeriveWorkstreamNonGit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if got, want := deriveWorkstream(), filepath.Base(dir); got != want {
		t.Errorf("non-git derive = %q, want cwd basename %q", got, want)
	}
}

// TestDeriveWorkstreamGitBranch: inside a git repo, "auto" derives the checked-out
// branch — the natural identity for a worktree-per-branch setup.
func TestDeriveWorkstreamGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"checkout", "-q", "-b", "feat-z"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if got := gitBranch(); got != "feat-z" {
		t.Errorf("gitBranch() = %q, want feat-z", got)
	}
	if got := deriveWorkstream(); got != "feat-z" {
		t.Errorf("deriveWorkstream() = %q, want feat-z", got)
	}
}

// TestWorkstreamAutoSentinel: NT_WORKSTREAM=auto resolves via derivation, and the
// explicit "auto" arg does too.
func TestWorkstreamAutoSentinel(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	want := filepath.Base(dir)
	s := &server{}
	t.Setenv("NT_WORKSTREAM", "auto")
	if got := s.workstream(map[string]any{}); got != want {
		t.Errorf("env auto = %q, want %q", got, want)
	}
	t.Setenv("NT_WORKSTREAM", "")
	if got := s.workstream(map[string]any{"workstream": "auto"}); got != want {
		t.Errorf("arg auto = %q, want %q", got, want)
	}
}
