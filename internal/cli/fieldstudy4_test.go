package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixes from the round-4 multi-agent field study: the undo-after-note-edit
// guard, path-style titles beating --kind's folder, [defaults] source for
// notes, task dedup parity, the doctor near-dup acknowledgment, non-TTY editor
// guards, and note --project for recall's project boost.

// `nt undo` right after a note edit must refuse: note operations aren't
// journaled, so the "last change" it would revert is an older, unrelated TASK
// change. --force proceeds and reverts that task change.
func TestUndoRefusedAfterNoteEdit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "add", "task to revert")
	captureRun(t, "note", "scratch pad", "--body", "v1")
	captureRun(t, "edit", "scratch pad", "--append", "v2")

	// Tests run inside one second; age the note edit past the grace window the
	// way real usage looks (the note edited well after the task change).
	var notes []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(captureRun(t, "notes", "--json")), &notes); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(10 * time.Second)
	for _, n := range notes {
		if err := os.Chtimes(n.Path, later, later); err != nil {
			t.Fatal(err)
		}
	}

	_, stderr, code := runWithStderr("undo")
	if code == 0 {
		t.Fatal("undo right after a note edit must be refused")
	}
	if !strings.Contains(stderr, "NOTE edit") || !strings.Contains(stderr, "scratch-pad.md") {
		t.Fatalf("refusal should name the note that was edited:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"add"`) || !strings.Contains(stderr, "--force") {
		t.Fatalf("refusal should name the task change undo WOULD revert and the --force escape:\n%s", stderr)
	}
	// Nothing was reverted.
	if out := captureRun(t, "list", "--json"); !strings.Contains(out, "task to revert") {
		t.Fatalf("refused undo must not touch the task:\n%s", out)
	}

	// --force reverts the TASK change (the note is untouched — not journaled).
	out := captureRun(t, "undo", "--force")
	if !strings.Contains(out, "undid: add") {
		t.Fatalf("undo --force should revert the task add:\n%s", out)
	}
	if out := captureRun(t, "list", "--json"); strings.Contains(out, "task to revert") {
		t.Fatalf("task should be gone after undo --force:\n%s", out)
	}
	if body := captureRun(t, "show", "scratch pad"); !strings.Contains(body, "v2") {
		t.Fatalf("the note itself must survive the undo:\n%s", body)
	}
}

// A plain undo with no later note edit still works untouched.
func TestUndoStillWorksWithoutLaterNoteEdit(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "note", "older note", "--body", "context") // predates the task change
	captureRun(t, "add", "revert me")
	out := captureRun(t, "undo")
	if !strings.Contains(out, "undid: add") {
		t.Fatalf("plain undo should revert the task add:\n%s", out)
	}
}

// A path-style title is an explicit filing choice: it beats --kind's canonical
// folder (which remains only a default), while the kind's tag still applies.
func TestNotePathStyleTitleBeatsKindFolder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	out := captureRun(t, "note", "custom/x", "--kind", "lesson")
	if !strings.Contains(out, "notes/custom/x.md") {
		t.Fatalf("path-style title should file under custom/, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "custom", "x.md")); err != nil {
		t.Fatalf("note not created at notes/custom/x.md: %v", err)
	}
	var j struct {
		Tags []string `json:"tags"`
	}
	json.Unmarshal([]byte(captureRun(t, "show", "x", "--json")), &j)
	if !contains(j.Tags, "lesson") {
		t.Fatalf("--kind lesson should still tag the note, got %v", j.Tags)
	}
	// Without a path or --folder, the kind's folder still applies (unchanged).
	out = captureRun(t, "note", "plain lesson capture", "--kind", "lesson")
	if !strings.Contains(out, "notes/lessons/plain-lesson-capture.md") {
		t.Fatalf("kind folder should still be the default: %s", out)
	}
}

// [defaults] source in config.toml sets nt note's default source, exactly like
// it already did for nt add; an explicit --source still wins.
func TestNoteDefaultSourceFromConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[defaults]\nsource = \"teambot\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	captureRun(t, "note", "configured source note", "--body", "b")
	var j struct {
		Source string `json:"source"`
	}
	json.Unmarshal([]byte(captureRun(t, "show", "configured source note", "--json")), &j)
	if j.Source != "teambot" {
		t.Fatalf("note should default to the configured source, got %q", j.Source)
	}
	captureRun(t, "note", "explicit source note", "--source", "byhand", "--body", "b")
	json.Unmarshal([]byte(captureRun(t, "show", "explicit source note", "--json")), &j)
	if j.Source != "byhand" {
		t.Fatalf("an explicit --source must win over config, got %q", j.Source)
	}
}

// Identical bare tasks (no shared tag/project — no overlap signal at all) still
// warn when the titles are near-identical, and both hints end with the
// verify-with-show caveat (the store may have changed since the warning).
func TestBareDuplicateTaskWarns(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "add", "fix the flaky login test")
	_, stderr, code := runWithStderr("add", "fix the flaky login test")
	if code != 0 {
		t.Fatalf("duplicate add should still succeed (warning is non-blocking), got %d", code)
	}
	if !strings.Contains(stderr, "similar task already exists") {
		t.Fatalf("identical bare tasks should warn without a shared tag/project:\n%s", stderr)
	}
	if !strings.Contains(stderr, "— the store may have changed since)") || !strings.Contains(stderr, "verify with `nt show ") {
		t.Fatalf("the dedup hint should end with the verify-with-show caveat:\n%s", stderr)
	}
	// A clearly different bare task stays silent.
	_, stderr, _ = runWithStderr("add", "renew the TLS certificates")
	if strings.Contains(stderr, "similar task") {
		t.Fatalf("unrelated task must not warn:\n%s", stderr)
	}
}

// nt doctor lints pairs of open tasks with near-identical titles as possible
// duplicates (informational, never a failure); completing one clears it.
func TestDoctorFlagsDuplicateOpenTasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "add", "ship the webhook retry queue")
	captureRun(t, "add", "ship the webhook retry queue")

	out := captureRun(t, "doctor", "--check") // informational → still exit 0
	if !strings.Contains(out, "possible duplicate open tasks") {
		t.Fatalf("doctor should flag duplicate open tasks:\n%s", out)
	}

	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	captureRun(t, "done", listed[0].ID)
	out = captureRun(t, "doctor", "--check")
	if strings.Contains(out, "possible duplicate open tasks") {
		t.Fatalf("a completed twin is no longer a duplicate OPEN task:\n%s", out)
	}
}

// Tagging either note of a near-duplicate pair `distinct` acknowledges the
// deliberate fork: doctor stops flagging it (the nag had no exit before).
func TestDoctorNearDupDistinctTag(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Token storage in cookie", "--tag", "auth", "--description", "a")
	captureRun(t, "note", "Token storage cookie approach", "--tag", "auth", "--force", "--description", "b")

	out := captureRun(t, "doctor", "--check")
	if !strings.Contains(out, "near-duplicate") {
		t.Fatalf("the fork should be flagged before acknowledgment:\n%s", out)
	}
	if !strings.Contains(out, "tag one 'distinct' to acknowledge a deliberate fork") {
		t.Fatalf("the near-dup notice should teach the acknowledgment escape:\n%s", out)
	}

	captureRun(t, "tag", "token-storage-cookie-approach", "+distinct")
	out = captureRun(t, "doctor", "--check")
	if strings.Contains(out, "near-duplicate") {
		t.Fatalf("a 'distinct'-tagged fork must not be re-flagged:\n%s", out)
	}
}

// `nt journal` without a terminal refuses before creating anything — $EDITOR
// into a pipe just spews escape sequences.
func TestJournalRefusesWithoutTerminal(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("EDITOR", "true")
	_, stderr, code := runWithStderr("journal")
	if code == 0 {
		t.Fatal("journal without a terminal should fail")
	}
	if !strings.Contains(stderr, "no terminal") || !strings.Contains(stderr, "nt note --folder journal") {
		t.Fatalf("journal should explain and point at the non-interactive alternative:\n%s", stderr)
	}
	if out := captureRun(t, "notes"); strings.Contains(out, "journal/") {
		t.Fatalf("the refused journal run must not create today's note:\n%s", out)
	}
}

// nt note --project stores project: frontmatter, and recall --project treats it
// as project membership (ProjectMatch boost) — no tag/folder required.
func TestNoteProjectFlagRecallBoost(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "note", "cache invalidation strategy", "--project", "gamma",
		"--body", "the gamma cache design", "--description", "how invalidation works")

	var res []struct {
		Title        string `json:"title"`
		ProjectMatch bool   `json:"projectMatch"`
	}
	json.Unmarshal([]byte(captureRun(t, "recall", "improving the cache invalidation layer", "--project", "gamma", "--json")), &res)
	if len(res) == 0 || !res[0].ProjectMatch {
		t.Fatalf("--project frontmatter should fire recall's project boost, got %+v", res)
	}
}
