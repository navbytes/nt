package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// idOf adds a task and returns its short id via list --json (the add line is
// noisy to parse).
func taskIDByText(t *testing.T, text string) string {
	t.Helper()
	var listed []taskJSON
	if err := json.Unmarshal([]byte(captureRun(t, "list", "--json", "--all")), &listed); err != nil {
		t.Fatal(err)
	}
	for _, j := range listed {
		if j.Text == text || strings.HasPrefix(j.Text, text) {
			return j.ID
		}
	}
	t.Fatalf("no task matching %q in %v", text, listed)
	return ""
}

// TestSearchAndTerms: multi-word queries AND their terms (order-independent),
// not match only an exact contiguous phrase — the headline findability fix.
func TestSearchAndTerms(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Zustand decision", "--folder", "decisions",
		"--body", "We chose Zustand for state management; it drops Redux boilerplate.")

	if out := captureRun(t, "search", "state management"); !strings.Contains(out, "Zustand decision") {
		t.Fatalf("AND search 'state management' should find the note:\n%s", out)
	}
	if out := captureRun(t, "search", "redux zustand"); !strings.Contains(out, "Zustand decision") {
		t.Fatalf("order-independent AND search should find the note:\n%s", out)
	}
	// A term that doesn't appear at all excludes the note.
	if out := captureRun(t, "search", "zustand kafka"); strings.Contains(out, "Zustand decision") {
		t.Fatalf("AND search must require every term:\n%s", out)
	}
	// Quoted phrase must be contiguous.
	if out := captureRun(t, "search", `"state management"`); !strings.Contains(out, "Zustand decision") {
		t.Fatalf("quoted contiguous phrase should match:\n%s", out)
	}
}

// TestSearchJSON: search emits structured JSON with notes (snippet) and tasks.
func TestSearchJSON(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Auth design", "--folder", "ref", "--body", "JWT tokens expire after 24h.")
	captureRun(t, "add", "fix jwt refresh", "--tag", "auth")

	out := captureRun(t, "search", "jwt", "--json")
	var res searchJSON
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("search --json not valid JSON: %v\n%s", err, out)
	}
	if len(res.Notes) != 1 || res.Notes[0].Title != "Auth design" {
		t.Fatalf("expected one note hit, got %+v", res.Notes)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("expected one task hit, got %+v", res.Tasks)
	}
}

// TestUpdateTitleTagsProject: `nt update` can change a task's title, tags, and
// project without an $EDITOR round-trip, preserving links.
func TestUpdateTitleTagsProject(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "Integate Strpe", "--tag", "backend", "--project", "payments")
	id := taskIDByText(t, "Integate Strpe")

	captureRun(t, "update", id, "--title", "Integrate Stripe SDK",
		"--tag", "urgent", "--untag", "backend", "--project", "billing")

	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	got := listed[0]
	if !strings.Contains(got.Text, "Integrate Stripe SDK") {
		t.Fatalf("title not updated: %q", got.Text)
	}
	if got.Project != "billing" {
		t.Fatalf("project not updated: %q", got.Project)
	}
	if !contains(got.Tags, "urgent") || contains(got.Tags, "backend") {
		t.Fatalf("tags not updated: %v", got.Tags)
	}
}

// TestTagAcceptsTasks: `nt tag <task-id> +x -y` retags a task (not just notes).
func TestTagAcceptsTasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "refactor auth")
	id := taskIDByText(t, "refactor auth")
	captureRun(t, "tag", id, "+reviewed")

	var listed []taskJSON
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	if !contains(listed[0].Tags, "reviewed") {
		t.Fatalf("nt tag should add a tag to a task: %v", listed[0].Tags)
	}
}

// TestNotesAndShow: `nt notes` lists notes and `nt show` prints one's body.
func TestNotesAndShow(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Cutover plan", "--folder", "decisions", "--body", "Dual-auth for two weeks.")

	if out := captureRun(t, "notes"); !strings.Contains(out, "decisions/cutover-plan.md") {
		t.Fatalf("nt notes should list the note:\n%s", out)
	}
	if out := captureRun(t, "notes", "--folder", "ref"); !strings.Contains(out, "no notes") {
		t.Fatalf("folder filter should exclude the decisions note:\n%s", out)
	}
	if out := captureRun(t, "show", "Cutover plan"); !strings.Contains(out, "Dual-auth for two weeks.") {
		t.Fatalf("nt show should print the body:\n%s", out)
	}
}

// TestEditNoteBareHandle: `nt edit <slug>` resolves a note without the note:
// prefix (it should not error with "no task").
func TestEditNoteBareHandle(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Some ref", "--folder", "ref")
	t.Setenv("EDITOR", "true") // a no-op editor so runEditor succeeds
	if out, code := runWithStdout("edit", "some-ref"); code != 0 {
		t.Fatalf("edit of a bare note handle failed (%d): %s", code, out)
	}
}

// TestLinksJSONDeps: `nt links --json` exposes subtask/blocked-by edges, not just
// wikilinks, and keeps raw ULID tokens out of linkedFrom.
func TestLinksJSONDeps(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "Design data layer")
	parent := taskIDByText(t, "Design data layer")
	captureRun(t, "add", "Add outbox table", "--parent", parent)
	captureRun(t, "add", "Ship gateway", "--blocks", parent)

	out := captureRun(t, "links", parent, "--json")
	var res linksJSON
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("links --json invalid: %v\n%s", err, out)
	}
	if len(res.Children) != 1 || res.Children[0].Title != "Add outbox table" {
		t.Fatalf("expected one child, got %+v", res.Children)
	}
	if len(res.BlockedBy) != 1 || res.BlockedBy[0].Title != "Ship gateway" {
		t.Fatalf("expected one blocked-by, got %+v", res.BlockedBy)
	}
	for _, b := range res.LinkedFrom {
		if strings.Contains(b.Text, "parent:") || strings.Contains(b.Text, "blocks:") {
			t.Fatalf("raw dependency tokens leaked into linkedFrom: %q", b.Text)
		}
	}
}

// TestRmNoteUnlink: deleting a note with --unlink strips inbound [[links]] so
// nothing dangles, then trashes the note.
func TestRmNoteUnlink(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Target", "--folder", "ref", "--body", "x")
	captureRun(t, "note", "Referrer", "--folder", "ref", "--body", "see [[Target]] here")

	if out := captureRun(t, "rm", "Target", "--unlink"); !strings.Contains(out, "deleted") {
		t.Fatalf("rm --unlink should delete:\n%s", out)
	}
	if out := captureRun(t, "show", "Referrer"); strings.Contains(out, "[[Target]]") {
		t.Fatalf("inbound link should have been stripped:\n%s", out)
	}
}

// TestRmNoteGuard: deleting a note with inbound links and no flags refuses
// (non-interactive), pointing at --unlink/--force rather than dangling silently.
func TestRmNoteGuard(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Target", "--folder", "ref", "--body", "x")
	captureRun(t, "note", "Referrer", "--folder", "ref", "--body", "see [[Target]]")

	out, code := runWithStdout("rm", "Target")
	if code == 0 {
		t.Fatalf("rm of a linked note must not silently succeed:\n%s", out)
	}
}
