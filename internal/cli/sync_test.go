package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitCmd runs a git command rooted at dir, failing the test on error (unless
// wantErr is checked by the caller via gitCmdErr).
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmdErr(dir, args...)
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

func gitCmdErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitConfigIdentity sets a LOCAL (not global) git identity for dir's repo —
// CI runners have no global user.name/user.email configured, and `nt sync`
// itself commits, so every repo these tests create (including clones) needs
// its own identity rather than relying on the environment's global config.
func gitConfigIdentity(t *testing.T, dir string) {
	t.Helper()
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test")
}

func TestSyncRequiresGitInit(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "hello") // give the store something, though it shouldn't matter
	out, code := runWithStdout("sync")
	if code == 0 {
		t.Fatalf("sync without a git repo should fail, got output: %s", out)
	}
}

// The core claim: two team members sharing an $NT_DIR over git converge via
// `nt sync` alone — commit, pull, reconcile, push — with no manual git steps
// beyond the one-time `nt git-init` + remote setup.
func TestSyncRoundTripBetweenTwoClones(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "team.git")
	gitCmd(t, t.TempDir(), "init", "--bare", bare)

	// Alice: git-init the store, seed a note, push it as the shared history.
	alice := t.TempDir()
	t.Setenv("NT_DIR", alice)
	captureRun(t, "note", "Alice's note", "--tag", "alice")
	captureRun(t, "git-init")
	gitConfigIdentity(t, alice)
	gitCmd(t, alice, "add", "-A")
	gitCmd(t, alice, "commit", "-m", "seed")
	gitCmd(t, alice, "remote", "add", "origin", bare)
	gitCmd(t, alice, "push", "-u", "origin", "HEAD")

	// Bob: clone the shared store, add his own note, sync (pull — trivially
	// up to date — reconcile, push).
	bob := filepath.Join(t.TempDir(), "bob")
	gitCmd(t, t.TempDir(), "clone", bare, bob)
	gitConfigIdentity(t, bob)
	t.Setenv("NT_DIR", bob)
	captureRun(t, "note", "Bob's note", "--tag", "bob")
	out := captureRun(t, "sync")
	if !strings.Contains(out, "synced") {
		t.Fatalf("bob's sync should report success:\n%s", out)
	}

	// Alice: create a third note, then sync — must pull bob's note first
	// (merge, no conflict: different files), then push her own.
	t.Setenv("NT_DIR", alice)
	captureRun(t, "note", "Quarterly planning doc", "--tag", "alice")
	out = captureRun(t, "sync")
	if !strings.Contains(out, "synced") {
		t.Fatalf("alice's sync should report success:\n%s", out)
	}
	list := captureRun(t, "search", "--tag", "bob", "--type", "note")
	if !strings.Contains(list, "Bob's note") {
		t.Fatalf("alice's store should have bob's note after sync:\n%s", list)
	}

	// A fresh clone should now see all three notes — the shared history the
	// pattern promises.
	carol := filepath.Join(t.TempDir(), "carol")
	gitCmd(t, t.TempDir(), "clone", bare, carol)
	t.Setenv("NT_DIR", carol)
	idx := captureRun(t, "index", "--all")
	for _, want := range []string{"Alice's note", "Bob's note", "Quarterly planning doc"} {
		if !strings.Contains(idx, want) {
			t.Errorf("carol's fresh clone missing %q:\n%s", want, idx)
		}
	}
}

func TestSyncCommitsLocalEditsBeforePulling(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "team.git")
	gitCmd(t, t.TempDir(), "init", "--bare", bare)

	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Seed")
	captureRun(t, "git-init")
	gitConfigIdentity(t, dir)
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "seed")
	gitCmd(t, dir, "remote", "add", "origin", bare)
	gitCmd(t, dir, "push", "-u", "origin", "HEAD")

	captureRun(t, "note", "Uncommitted note")
	if status := gitCmd(t, dir, "status", "--porcelain"); status == "" {
		t.Fatal("test setup broken: expected an uncommitted change before sync")
	}
	captureRun(t, "sync")
	if status := gitCmd(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("sync should commit local edits, tree still dirty:\n%s", status)
	}
	log := gitCmd(t, dir, "log", "--oneline")
	if !strings.Contains(log, "nt sync") {
		t.Fatalf("expected an 'nt sync' commit in the log:\n%s", log)
	}
}

func TestSyncNoPushSkipsPush(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "team.git")
	gitCmd(t, t.TempDir(), "init", "--bare", bare)

	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Seed")
	captureRun(t, "git-init")
	gitConfigIdentity(t, dir)
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "seed")
	gitCmd(t, dir, "remote", "add", "origin", bare)
	gitCmd(t, dir, "push", "-u", "origin", "HEAD")

	captureRun(t, "note", "Local only")
	out := captureRun(t, "sync", "--no-push")
	if !strings.Contains(out, "not pushed") {
		t.Fatalf("--no-push should say so:\n%s", out)
	}

	other := filepath.Join(t.TempDir(), "other")
	gitCmd(t, t.TempDir(), "clone", bare, other)
	t.Setenv("NT_DIR", other)
	idx := captureRun(t, "index", "--all")
	if strings.Contains(idx, "Local only") {
		t.Fatal("--no-push must not have pushed the local commit")
	}
}
