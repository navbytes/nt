package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestStartStopTracking(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "add", "deep work", "--est", "1h")
	id := reviewTaskID(t, "deep work")

	captureRun(t, "start", id)
	if !strings.Contains(captureRun(t, "list"), "est:1h") {
		t.Error("est should be stored and shown")
	}

	// Back-date the running timer by 30 minutes so stop logs real elapsed time.
	tf := filepath.Join(dir, "tasks.txt")
	raw, _ := os.ReadFile(tf)
	s := regexp.MustCompile(`started:(\d+)`).ReplaceAllStringFunc(string(raw), func(m string) string {
		n, _ := strconv.ParseInt(m[len("started:"):], 10, 64)
		return "started:" + strconv.FormatInt(n-1800, 10)
	})
	if err := os.WriteFile(tf, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureRun(t, "stop", id)
	if !strings.Contains(out, "logged 30m") {
		t.Errorf("stop should log ~30m, got: %s", out)
	}
	list := captureRun(t, "list")
	if !strings.Contains(list, "spent:30m") {
		t.Errorf("list should show spent:30m:\n%s", list)
	}
	// started: is cleared after stop.
	if after, _ := os.ReadFile(tf); strings.Contains(string(after), "started:") {
		t.Error("stop should clear the started: timer")
	}

	// stop on an untracked task is refused (non-zero exit).
	captureRun(t, "add", "idle")
	if _, code := runWithStdout("stop", reviewTaskID(t, "idle")); code == 0 {
		t.Error("stop on an untracked task should fail")
	}
}
