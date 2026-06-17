package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigDefaultPrioritySource: [defaults] priority/source in config.toml set
// the defaults for `nt add` (an explicit flag still wins).
func TestConfigDefaultPrioritySource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	cfg := "[defaults]\npriority = \"high\"\nsource = \"agent\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	captureRun(t, "add", "configured default")
	captureRun(t, "add", "explicit wins", "--pri", "low", "--source", "cli")

	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json", "--all")), &listed)
	byText := map[string]taskJSON{}
	for _, j := range listed {
		byText[j.Text] = j
	}
	if g := byText["configured default"]; g.Priority != "A" || g.Source != "agent" {
		t.Fatalf("config defaults not applied: pri=%q src=%q", g.Priority, g.Source)
	}
	if g := byText["explicit wins"]; g.Priority != "C" || g.Source != "cli" {
		t.Fatalf("explicit flags should override config: pri=%q src=%q", g.Priority, g.Source)
	}
}

// TestUpdateSource: `nt update <id> --source X` sets the source; --source none
// clears it.
func TestUpdateSource(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "retag source")
	id := taskIDByText(t, "retag source")

	captureRun(t, "update", id, "--source", "claude")
	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	if listed[0].Source != "claude" {
		t.Fatalf("update --source not applied: %q", listed[0].Source)
	}

	captureRun(t, "update", id, "--source", "none")
	var after []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &after)
	if after[0].Source != "" {
		t.Fatalf("update --source none should clear source: %q", after[0].Source)
	}
}

// TestListSource: `nt list --source X` filters by source for parity with
// ready/agenda/recall/log.
func TestListSource(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "from claude", "--source", "claude")
	captureRun(t, "add", "from cli", "--source", "cli")

	out := captureRun(t, "list", "--source", "claude", "--json")
	var listed []taskJSON
	json.Unmarshal([]byte(out), &listed)
	if len(listed) != 1 || listed[0].Text != "from claude" {
		t.Fatalf("list --source should filter to claude, got %+v", listed)
	}
}

// TestTagsJSON: `nt tags --json` emits [{tag,count}].
func TestTagsJSON(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "x", "--tag", "alpha")
	captureRun(t, "add", "y", "--tag", "alpha", "--tag", "beta")

	out := captureRun(t, "tags", "--json")
	var rows []tagCount
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("tags --json invalid: %v\n%s", err, out)
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.Tag] = r.Count
	}
	if got["alpha"] != 2 || got["beta"] != 1 {
		t.Fatalf("unexpected tag counts: %v", got)
	}
}

// TestMvRejectsExtraPositional: `nt mv <note> a b` errors instead of silently
// joining "a b" into the filename.
func TestMvRejectsExtraPositional(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Movable", "--folder", "ref")
	if _, code := runWithStdout("mv", "Movable", "newname", "stray"); code == 0 {
		t.Fatal("mv with an extra positional should be rejected")
	}
}

// TestDoneRecurrenceNote: completing a recurring task reports the spawned next
// occurrence.
func TestDoneRecurrenceNote(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "standup", "--recur", "weekly", "--due", "today")
	id := taskIDByText(t, "standup")
	out := captureRun(t, "done", id)
	if !strings.Contains(out, "next occurrence") {
		t.Fatalf("done of a recurring task should note the next occurrence:\n%s", out)
	}
}

// TestStopRestoresPriorState: a task that was blocked before `nt start` returns
// to blocked after `nt stop`, not forced to open.
func TestStopRestoresPriorState(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "blocked work")
	id := taskIDByText(t, "blocked work")
	captureRun(t, "update", id, "--status", "blocked")
	captureRun(t, "start", id)
	captureRun(t, "stop", id)

	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json", "--show-blocked")), &listed)
	if listed[0].Status != "blocked" {
		t.Fatalf("stop should restore the pre-start state (blocked), got %q", listed[0].Status)
	}
}
