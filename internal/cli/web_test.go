package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// deadPID spawns a short-lived process and reaps it, returning a PID that is now
// guaranteed dead — a deterministic input for liveness checks.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a helper process: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Kill()
	_ = cmd.Wait() // reap so the PID is fully gone
	return pid
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("the test process itself should read as alive")
	}
	if processAlive(deadPID(t)) {
		t.Error("a killed+reaped process should read as dead")
	}
}

// TestDetachChildArgsForcesResolvedEdit guards the resolved edit decision across
// the detach re-exec: the child re-reads config, so the parent must pass --edit
// OR --read-only explicitly — otherwise `--detach --read-only` over a `[web]
// edit = true` store would come up editable.
func TestDetachChildArgsForcesResolvedEdit(t *testing.T) {
	has := func(args []string, want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	editable := detachChildArgs("127.0.0.1", 4321, true)
	if !has(editable, "--edit") || has(editable, "--read-only") {
		t.Errorf("editable detach args = %v, want --edit (and not --read-only)", editable)
	}
	readonly := detachChildArgs("127.0.0.1", 4321, false)
	if !has(readonly, "--read-only") || has(readonly, "--edit") {
		t.Errorf("read-only detach args = %v, want --read-only (and not --edit)", readonly)
	}
}

func TestWebProcRoundTrip(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	in := &webProc{PID: 4242, URL: "http://127.0.0.1:4321", Edit: true, Started: "2026-06-09T00:00:00Z"}
	if err := writeWebProc(in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readWebProc()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if *got != *in {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, in)
	}
}

func TestWebStatusAndStopWhenIdle(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())

	out, code := runWithStdout("web", "--status")
	if code != 1 || !strings.Contains(out, "not running") {
		t.Errorf("status with no server: code=%d out=%q", code, out)
	}
	out, code = runWithStdout("web", "--stop")
	if code != 0 || !strings.Contains(out, "not running") {
		t.Errorf("stop with no server: code=%d out=%q", code, out)
	}
}

func TestWebStopClearsStalePid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	if err := writeWebProc(&webProc{PID: deadPID(t), URL: "http://127.0.0.1:4321"}); err != nil {
		t.Fatal(err)
	}

	out, code := runWithStdout("web", "--stop")
	if code != 0 || !strings.Contains(out, "stale") {
		t.Errorf("stop with a dead pid should clear the stale file: code=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, webPidFile)); !os.IsNotExist(err) {
		t.Error("the stale PID file should have been removed")
	}
	// And status agrees it's gone.
	if out, code := runWithStdout("web", "--status"); code != 1 || !strings.Contains(out, "not running") {
		t.Errorf("status after stale clear: code=%d out=%q", code, out)
	}
}
