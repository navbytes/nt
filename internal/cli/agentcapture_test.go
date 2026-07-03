package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The capture-ergonomics surface added after the multi-agent field study:
// bodies on add/update, file/stdin bodies, non-interactive note edits,
// --blocked-by, and edge clearing. These are the paths agents (not humans in
// $EDITOR) live on.

func TestAddBodyCreatesLinkedDetailNote(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	out := captureRun(t, "add", "fix token refresh race", "--body", "## Plan\nsingle-flight the refresh", "--json")
	var created struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("add --json: %v\n%s", err, out)
	}
	if !strings.Contains(created.Text, "[[fix token refresh race]]") {
		t.Fatalf("task text should link the detail note, got %q", created.Text)
	}
	// The body is readable back through the link target.
	shown := captureRun(t, "show", "fix token refresh race")
	if !strings.Contains(shown, "single-flight the refresh") {
		t.Fatalf("detail body not readable via show:\n%s", shown)
	}
}

func TestBodyFileAndStdin(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	f := filepath.Join(t.TempDir(), "body.md")
	hostile := "Decision: `os.Setenv` + $(dangerous) — survives the shell.\n"
	os.WriteFile(f, []byte(hostile), 0o644)
	captureRun(t, "note", "quoting survival", "--body-file", f)
	shown := captureRun(t, "show", "quoting survival")
	if !strings.Contains(shown, "$(dangerous)") || !strings.Contains(shown, "`os.Setenv`") {
		t.Fatalf("body-file content mangled:\n%s", shown)
	}
	// --body + --body-file is refused.
	if _, code := runWithStdout("note", "x y z", "--body", "a", "--body-file", f); code == 0 {
		t.Fatal("--body with --body-file should be an error")
	}
}

func TestEditAppendNonInteractive(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "running log", "--body", "first entry")
	captureRun(t, "edit", "running log", "--append", "second entry (resolved in commit abc123)")
	shown := captureRun(t, "show", "running log")
	if !strings.Contains(shown, "first entry") || !strings.Contains(shown, "second entry") {
		t.Fatalf("append lost content:\n%s", shown)
	}
	// Replace wholesale from a file.
	f := filepath.Join(t.TempDir(), "nb.md")
	os.WriteFile(f, []byte("replaced body\n"), 0o644)
	captureRun(t, "edit", "running log", "--body-file", f)
	shown = captureRun(t, "show", "running log")
	if strings.Contains(shown, "first entry") || !strings.Contains(shown, "replaced body") {
		t.Fatalf("body-file replace failed:\n%s", shown)
	}
}

func TestUpdateAttachNoteAndBody(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "implement wrap()")
	captureRun(t, "note", "wrap design", "--body", "queue + timer")
	var listed []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	id := listed[0].ID

	// Attach an existing note after the fact (the add-only asymmetry fix).
	captureRun(t, "update", id, "--note", "wrap design")
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	if !strings.Contains(listed[0].Text, "[[wrap design]]") {
		t.Fatalf("update --note didn't link, got %q", listed[0].Text)
	}
	// Attaching detail creates + links a fresh note.
	captureRun(t, "update", id, "--body", "edge cases: zero-capacity bucket")
	shown := captureRun(t, "show", "implement wrap()")
	if !strings.Contains(shown, "zero-capacity") {
		t.Fatalf("update --body detail not readable:\n%s", shown)
	}
	// A typo'd note handle fails loudly.
	if _, code := runWithStdout("update", id, "--note", "no-such-note"); code == 0 {
		t.Fatal("update --note with a bad handle should fail")
	}
}

func TestBlockedByAndClearEdge(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "design the API")
	captureRun(t, "add", "implement the API")
	var listed []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	design, impl := listed[0].ID, listed[1].ID

	// implement is blocked by design → ready hides implement.
	captureRun(t, "update", impl, "--blocked-by", design)
	ready := captureRun(t, "ready")
	if strings.Contains(ready, "implement the API") {
		t.Fatalf("blocked task visible in ready:\n%s", ready)
	}
	// Clearing with --blocks none on the BLOCKER unhides it.
	captureRun(t, "update", design, "--blocks", "none")
	ready = captureRun(t, "ready")
	if !strings.Contains(ready, "implement the API") {
		t.Fatalf("cleared edge still hides task:\n%s", ready)
	}
	// Unresolvable ids fail loudly instead of silently dropping the edge.
	if _, code := runWithStdout("update", impl, "--blocked-by", "ZZZZZZ"); code == 0 {
		t.Fatal("update --blocked-by with a bogus id should fail")
	}
	if _, code := runWithStdout("add", "task w/ bad dep", "--blocks", "ZZZZZZ"); code == 0 {
		t.Fatal("add --blocks with a bogus id should fail")
	}
}

func TestAddBlockedByFlag(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "decide schema")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	captureRun(t, "add", "run migration", "--blocked-by", listed[0].ID)
	ready := captureRun(t, "ready")
	if strings.Contains(ready, "run migration") {
		t.Fatalf("add --blocked-by task should start hidden:\n%s", ready)
	}
	captureRun(t, "done", listed[0].ID)
	ready = captureRun(t, "ready")
	if !strings.Contains(ready, "run migration") {
		t.Fatalf("task should unhide when its blocker completes:\n%s", ready)
	}
}

func TestTagsAliasCommaSeparated(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "tag me", "--tags", "auth,backend")
	var listed []struct {
		Tags []string `json:"tags"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	if len(listed[0].Tags) != 2 {
		t.Fatalf("expected 2 tags from --tags a,b, got %v", listed[0].Tags)
	}
}

func TestNoteKindSteersTaxonomy(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "chose sentinel error over bool", "--kind", "decision", "--description", "x")
	var j struct {
		Path string `json:"path"`
		Tags []string `json:"tags"`
	}
	json.Unmarshal([]byte(captureRun(t, "show", "chose sentinel error over bool", "--json")), &j)
	if !strings.Contains(j.Path, "decisions/") {
		t.Fatalf("--kind decision should file under decisions/, got %s", j.Path)
	}
	found := false
	for _, tg := range j.Tags {
		if tg == "decision" {
			found = true
		}
	}
	if !found {
		t.Fatalf("--kind decision should tag 'decision', got %v", j.Tags)
	}
	if _, code := runWithStdout("note", "bad kind", "--kind", "todo"); code == 0 {
		t.Fatal("invalid --kind should be refused")
	}
}
