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
