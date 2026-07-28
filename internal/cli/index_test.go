package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestIndexListsStubsNotBodies(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "JWT design", "--folder", "ref", "--tag", "auth",
		"--description", "24h tokens, 7d refresh", "--body", "secret body detail here")
	captureRun(t, "add", "wire refresh endpoint", "--tag", "auth")

	out := captureRun(t, "index")
	if !strings.Contains(out, "JWT design") || !strings.Contains(out, "24h tokens, 7d refresh") {
		t.Errorf("index should show title + description:\n%s", out)
	}
	if strings.Contains(out, "secret body detail here") {
		t.Errorf("index must NOT include note bodies:\n%s", out)
	}
	if !strings.Contains(out, "wire refresh endpoint") {
		t.Errorf("index should list active tasks:\n%s", out)
	}
}

func TestIndexJSONShapeAndDescriptionFallback(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Explicit", "--folder", "ref", "--description", "set explicitly", "--body", "body one")
	captureRun(t, "note", "Fallback", "--folder", "ref", "--body", "first body line becomes the description")

	var payload struct {
		Notes []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Folder      string `json:"folder"`
		} `json:"notes"`
	}
	if err := json.Unmarshal([]byte(captureRun(t, "index", "--json")), &payload); err != nil {
		t.Fatalf("index --json did not parse: %v", err)
	}
	byTitle := map[string]string{}
	for _, n := range payload.Notes {
		if n.ID == "" || n.Folder != "ref" {
			t.Errorf("stub missing id/folder: %+v", n)
		}
		byTitle[n.Title] = n.Description
	}
	if byTitle["Explicit"] != "set explicitly" {
		t.Errorf("explicit description wrong: %q", byTitle["Explicit"])
	}
	if byTitle["Fallback"] != "first body line becomes the description" {
		t.Errorf("description should fall back to first body line: %q", byTitle["Fallback"])
	}
}

func TestIndexFolderScope(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "In ref", "--folder", "ref", "--body", "x")
	captureRun(t, "note", "In decisions", "--folder", "decisions", "--body", "y")

	out := captureRun(t, "index", "--folder", "ref")
	if !strings.Contains(out, "In ref") || strings.Contains(out, "In decisions") {
		t.Errorf("--folder ref should scope to ref only:\n%s", out)
	}
}

// nt doctor lints notes: an unresolved [[link]] is a dangling-link problem that
// makes --check exit non-zero.
func TestDoctorFlagsDanglingNoteLink(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Source", "--folder", "ref", "--body", "see [[ghost-note]]")

	out, code := runWithStdout("doctor", "--check")
	if code == 0 {
		t.Fatalf("doctor --check should exit non-zero on a dangling link:\n%s", out)
	}
	if !strings.Contains(out, "dangling link") || !strings.Contains(out, "ghost-note") {
		t.Errorf("doctor should name the dangling link:\n%s", out)
	}
}

func TestDoctorHealthyStoreWithGoodLinks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Target", "--folder", "ref", "--description", "d", "--body", "x")
	captureRun(t, "note", "Source", "--folder", "ref", "--description", "d", "--body", "see [[target]]")

	out, code := runWithStdout("doctor", "--check")
	if code != 0 {
		t.Fatalf("doctor --check should pass when links resolve:\n%s", out)
	}
	if strings.Contains(out, "dangling") {
		t.Errorf("no dangling links expected:\n%s", out)
	}
}

// The empty-state line must say plainly that BOTH the note and task halves
// are empty, not just nudge toward `nt add` — agents reading only this line
// mistook the old tasks-only copy for proof notes lived somewhere else.
func TestIndexEmptyStoreMentionsBothNotesAndTasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())

	out := captureRun(t, "index")
	if !strings.Contains(out, "0 notes") || !strings.Contains(out, "0 tasks") {
		t.Errorf("empty index should state both halves are empty:\n%s", out)
	}
	if !strings.Contains(out, "nt note") {
		t.Errorf("empty index should point at the note entry point too:\n%s", out)
	}
}

// 0 notes but N tasks is a partially-empty case that must still say "no
// notes" — the old code printed nothing about notes at all, so a reader of
// only the body (not the header comment) couldn't tell "no notes exist"
// from "index doesn't show notes here".
func TestIndexNoNotesButTasksSaysSo(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "wire refresh endpoint")

	out := captureRun(t, "index")
	if !strings.Contains(out, "no notes") {
		t.Errorf("0-notes/N-tasks index should call out the empty note half:\n%s", out)
	}
}

// N notes but 0 tasks is the mirror case — must say "no active tasks", not
// silently omit the tasks section with no explanation.
func TestIndexNoTasksButNotesSaysSo(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "JWT design", "--folder", "ref", "--body", "x")

	out := captureRun(t, "index")
	if !strings.Contains(out, "no active tasks") {
		t.Errorf("N-notes/0-tasks index should call out the empty task half:\n%s", out)
	}
}

// --no-tasks means the caller deliberately excluded tasks, so an otherwise
// empty note catalog must not claim "0 tasks" — that would misreport a
// filter as the store's real state.
func TestIndexEmptyStoreWithNoTasksFlagStaysSilentOnTaskCount(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "a task that exists")

	out := captureRun(t, "index", "--no-tasks")
	if strings.Contains(out, "0 tasks") || strings.Contains(out, "1 task") {
		t.Errorf("--no-tasks empty-note-catalog message should not claim a task count:\n%s", out)
	}
	if !strings.Contains(out, "no notes") {
		t.Errorf("--no-tasks should still report the empty note half:\n%s", out)
	}
}

// nt index --project filters notes by `project:` frontmatter and tasks by
// their +project tag — a hard filter, unlike `recall --project`'s soft
// ranking preference (recall never excludes; index does).
func TestIndexProjectFiltersNotesAndTasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "wtcockpit note", "--folder", "ref", "--project", "wtcockpit", "--body", "x")
	captureRun(t, "note", "roost note", "--folder", "ref", "--project", "roost", "--body", "y")
	captureRun(t, "add", "wtcockpit task", "--project", "wtcockpit")
	captureRun(t, "add", "roost task", "--project", "roost")

	out := captureRun(t, "index", "--project", "wtcockpit")
	if !strings.Contains(out, "wtcockpit note") || strings.Contains(out, "roost note") {
		t.Errorf("--project should scope notes to the matching project:\n%s", out)
	}
	if !strings.Contains(out, "wtcockpit task") || strings.Contains(out, "roost task") {
		t.Errorf("--project should scope tasks to the matching project:\n%s", out)
	}
}

// nt index --project foo used to fail outright: "flag provided but not
// defined: -project". The flag must parse and produce a clean exit.
func TestIndexProjectFlagIsRecognized(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	out, code := runWithStdout("index", "--project", "anything")
	if code != 0 {
		t.Fatalf("index --project should be a recognized flag, got exit %d:\n%s", code, out)
	}
}

// nt index --limit caps the catalog and flags truncation.
func TestIndexLimitTruncates(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	for i := 0; i < 5; i++ {
		captureRun(t, "note", fmt.Sprintf("Distinct topic %d", i), "--force", "--folder", "ref", "--description", "d", "--body", "b")
	}
	out := captureRun(t, "index", "--limit", "2", "--json")
	var p struct {
		Notes     []map[string]any `json:"notes"`
		Truncated bool             `json:"truncated"`
		NoteTotal int              `json:"noteTotal"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) != 2 || !p.Truncated || p.NoteTotal != 5 {
		t.Fatalf("limit=2 → shown=%d truncated=%v total=%d", len(p.Notes), p.Truncated, p.NoteTotal)
	}
}
