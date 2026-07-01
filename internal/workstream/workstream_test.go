package workstream

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDeriveNonGit: outside a git repo, derivation falls back to the
// working-directory basename.
func TestDeriveNonGit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if got, want := Derive(), filepath.Base(dir); got != want {
		t.Errorf("non-git derive = %q, want cwd basename %q", got, want)
	}
}

// TestDeriveGitBranch: inside a git repo, derivation returns the checked-out
// branch — the natural identity for a worktree-per-branch setup.
func TestDeriveGitBranch(t *testing.T) {
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
	if got := Derive(); got != "feat-z" {
		t.Errorf("Derive() = %q, want feat-z", got)
	}
}

// TestEnv: NT_WORKSTREAM literal wins; "auto" derives; unset → "".
func TestEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("NT_WORKSTREAM", "feat-x")
	if got := Env(); got != "feat-x" {
		t.Errorf("literal env = %q, want feat-x", got)
	}
	t.Setenv("NT_WORKSTREAM", "auto")
	if got := Env(); got != filepath.Base(dir) {
		t.Errorf("auto env = %q, want cwd basename", got)
	}
	t.Setenv("NT_WORKSTREAM", "")
	if got := Env(); got != "" {
		t.Errorf("unset env = %q, want empty", got)
	}
}
