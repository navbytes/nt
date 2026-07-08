package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixes from the round-4 multi-agent field study: the undo-after-note-edit
// guard, path-style titles beating --kind's folder, [defaults] source for
// notes, task dedup parity, the doctor near-dup acknowledgment, non-TTY editor
// guards, and note --project for recall's project boost.

// `nt undo` right after a note edit reverts the NOTE, not an older unrelated
// task change: note edits are journaled (notes-undo.jsonl) exactly like tasks
// (undo.jsonl), and undo/redo act on whichever journal's pending entry is
// more recent — "the last thing I did," whether that touched a task or a note.
func TestUndoRevertsNoteEditWhenMostRecent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "add", "task to keep")
	captureRun(t, "note", "scratch pad", "--body", "v1")
	// Deliberately back-to-back, no artificial delay: this is agent-speed
	// usage (a scripted caller issuing add/note/edit inside the same wall-clock
	// second), and undo must pick the more recent journal entry WITHOUT
	// needing seconds of gap — both journals carry nanosecond-precision
	// timestamps precisely so this ordering is resolvable within one second.
	captureRun(t, "edit", "scratch pad", "--append", "v2")

	// The most recent action was the note edit — undo reverts THAT, not the
	// older task add.
	out := captureRun(t, "undo")
	if !strings.Contains(out, "note") || !strings.Contains(out, "scratch-pad.md") {
		t.Fatalf("undo should have reverted the note edit:\n%s", out)
	}
	if body := captureRun(t, "show", "scratch pad"); !strings.Contains(body, "v1") || strings.Contains(body, "v2") {
		t.Fatalf("note should be back to v1 after undo:\n%s", body)
	}
	if out := captureRun(t, "list", "--json"); !strings.Contains(out, "task to keep") {
		t.Fatalf("the task must be untouched by the note undo:\n%s", out)
	}

	// A second undo now reverts the older task add — the note journal's
	// pending entry just flipped to a redo, so it's no longer undo-eligible.
	out = captureRun(t, "undo")
	if !strings.Contains(out, "undid: add") {
		t.Fatalf("the second undo should revert the task add:\n%s", out)
	}
	if out := captureRun(t, "list", "--json"); strings.Contains(out, "task to keep") {
		t.Fatalf("task should be gone after the second undo:\n%s", out)
	}

	// Redo is LIFO: it replays the most-recently-undone action first (the
	// task add, undone second), then the note edit (undone first) — the same
	// timestamp comparison undo uses, just now over each journal's "redo:"
	// entry instead of its forward one.
	out = captureRun(t, "redo")
	if !strings.Contains(out, "redid: add") {
		t.Fatalf("the first redo should restore the task add:\n%s", out)
	}
	if out := captureRun(t, "list", "--json"); !strings.Contains(out, "task to keep") {
		t.Fatalf("task should be back after the first redo:\n%s", out)
	}
	out = captureRun(t, "redo")
	if !strings.Contains(out, "note") {
		t.Fatalf("the second redo should restore the note edit:\n%s", out)
	}
	if body := captureRun(t, "show", "scratch pad"); !strings.Contains(body, "v2") {
		t.Fatalf("note should be back to v2 after both redos:\n%s", body)
	}
}

// The shared-store ownership check applies to note reversals exactly like
// task ones: a note edit made by a different workstream is refused unless
// --force.
func TestUndoRefusesAnotherWorkstreamsNoteEdit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	t.Setenv("NT_WORKSTREAM", "agent-a")
	captureRun(t, "note", "shared note", "--body", "v1")
	captureRun(t, "edit", "shared note", "--append", "v2")

	t.Setenv("NT_WORKSTREAM", "agent-b")
	_, stderr, code := runWithStderr("undo")
	if code == 0 {
		t.Fatal("undoing another workstream's note edit must be refused without --force")
	}
	if !strings.Contains(stderr, "agent-a") || !strings.Contains(stderr, "--force") {
		t.Fatalf("refusal should name the owning workstream and the --force escape:\n%s", stderr)
	}
	if body := captureRun(t, "show", "shared note"); !strings.Contains(body, "v2") {
		t.Fatalf("refused undo must not touch the note:\n%s", body)
	}

	out := captureRun(t, "undo", "--force")
	if !strings.Contains(out, "note") {
		t.Fatalf("undo --force should revert the note edit:\n%s", out)
	}
	if body := captureRun(t, "show", "shared note"); !strings.Contains(body, "v1") {
		t.Fatalf("note should be back to v1 after undo --force:\n%s", body)
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

// `nt edit --body` replaces the whole body with LITERAL text — no temp file
// needed, unlike --body-file. This is the gap agents hit hardest: fixing a
// note used to mean either $EDITOR or writing the entire new body to a file.
func TestEditBodyInlineReplace(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "inline body test", "--body", "original")
	out := captureRun(t, "edit", "inline body test", "--body", "brand new body")
	if !strings.Contains(out, "replaced body of") {
		t.Fatalf("edit --body should report a replace, got %q", out)
	}
	shown := captureRun(t, "show", "inline body test")
	if !strings.Contains(shown, "brand new body") || strings.Contains(shown, "original") {
		t.Fatalf("body should be fully replaced:\n%s", shown)
	}
}

// `nt edit --old-string/--new-string` patches one exact match in the body
// in place — the targeted fix a longer note needs, without resending the
// whole body (the second real gap: --append only adds, never corrects).
func TestEditOldStringNewStringTargetedPatch(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "typo note", "--body", "Intro.\n\nThe cach layer needs work.\n\nConclusion.")
	out := captureRun(t, "edit", "typo note", "--old-string", "cach layer", "--new-string", "cache layer")
	if !strings.Contains(out, "edited") {
		t.Fatalf("edit --old-string/--new-string should report a patch, got %q", out)
	}
	shown := captureRun(t, "show", "typo note")
	if !strings.Contains(shown, "cache layer") || strings.Contains(shown, "cach layer") {
		t.Fatalf("targeted patch didn't apply cleanly:\n%s", shown)
	}
	if !strings.Contains(shown, "Intro.") || !strings.Contains(shown, "Conclusion.") {
		t.Fatalf("the rest of the body must survive untouched:\n%s", shown)
	}
}

// An ambiguous --old-string (matches more than once) refuses rather than
// guessing which occurrence to patch — the same discipline as this
// environment's own file-editing tool.
func TestEditOldStringAmbiguousRefuses(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "dup text note", "--body", "foo bar\nfoo bar")
	_, stderr, code := runWithStderr("edit", "dup text note", "--old-string", "foo bar", "--new-string", "baz")
	if code == 0 {
		t.Fatal("an --old-string matching twice must be refused, not guessed at")
	}
	if !strings.Contains(stderr, "2 times") {
		t.Fatalf("refusal should say how many times it matched:\n%s", stderr)
	}
	shown := captureRun(t, "show", "dup text note")
	if !strings.Contains(shown, "foo bar\nfoo bar") {
		t.Fatalf("an ambiguous match must leave the body untouched:\n%s", shown)
	}
}

// --old-string with no match, and the incomplete-pair case, both fail loudly
// rather than silently no-op-ing.
func TestEditOldStringNotFoundAndUnpaired(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "plain note", "--body", "some text here")

	_, stderr, code := runWithStderr("edit", "plain note", "--old-string", "nonexistent", "--new-string", "x")
	if code == 0 {
		t.Fatal("a non-matching --old-string must be refused")
	}
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("refusal should say the text wasn't found:\n%s", stderr)
	}

	_, stderr, code = runWithStderr("edit", "plain note", "--old-string", "some text")
	if code == 0 {
		t.Fatal("--old-string without --new-string must be rejected as a usage error")
	}
	if !strings.Contains(stderr, "together") {
		t.Fatalf("refusal should explain the pair requirement:\n%s", stderr)
	}
}

// --body/--body-file/--old-string+--new-string are three different ways to
// change a body; mixing them in one call is refused rather than silently
// picking one.
func TestEditBodyModesAreMutuallyExclusive(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "mixed modes note", "--body", "text")
	_, stderr, code := runWithStderr("edit", "mixed modes note", "--append", "more", "--body", "replace")
	if code == 0 {
		t.Fatal("mixing --append and --body must be rejected")
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("refusal should explain the conflict:\n%s", stderr)
	}
}
