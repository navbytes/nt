package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// runCaptureStderr runs a CLI invocation capturing BOTH streams, returning
// (stdout, stderr, code) — for asserting on warnings, which go to stderr so
// --json stdout stays parseable.
func runCaptureStderr(args ...string) (string, string, int) {
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	code := Run(args)
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	outB, _ := io.ReadAll(rOut)
	errB, _ := io.ReadAll(rErr)
	return string(outB), string(errB), code
}

// `nt index --project` must match case-insensitively: the note stores the
// casing the author typed, the caller guesses — the two disagreeing silently
// returned "0 notes" (which agents read as "those notes don't exist").
func TestIndexProjectCaseInsensitive(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "webhookd retry backoff", "--project", "WebhookD", "--tag", "design", "--description", "d")
	captureRun(t, "note", "unrelated note", "--tag", "misc", "--description", "d")

	var got struct {
		Notes []map[string]any `json:"notes"`
	}
	out := captureRun(t, "index", "--project", "webhookd", "--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 {
		t.Fatalf("--project should match case-insensitively, got %d notes: %s", len(got.Notes), out)
	}
	if title := got.Notes[0]["title"]; title != "webhookd retry backoff" {
		t.Fatalf("wrong note matched: %v", title)
	}
}

// `nt search --project` scopes notes (project: frontmatter) and tasks
// (+project) — including with no query at all, which lists the project's items.
// Frontmatter is invisible to the text match, so this is the only way search
// can scope by project.
func TestSearchProjectFilter(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "gateway retry design", "--project", "WebhookD", "--tag", "design", "--description", "d")
	captureRun(t, "note", "tripto retry checklist", "--project", "tripto", "--tag", "design", "--force", "--description", "d")
	captureRun(t, "add", "ship webhookd v2", "--project", "webhookd")
	captureRun(t, "add", "unrelated task")

	// Bare --project (no query) lists the project's items, both kinds.
	out := captureRun(t, "search", "--project", "webhookd", "--json")
	if !strings.Contains(out, "gateway retry design") || !strings.Contains(out, "ship webhookd v2") {
		t.Fatalf("bare --project should list the project's note and task: %s", out)
	}
	if strings.Contains(out, "tripto retry checklist") || strings.Contains(out, "unrelated task") {
		t.Fatalf("--project leaked items from outside the project: %s", out)
	}

	// Combined with a query it stays a hard filter: both notes match "retry",
	// only the scoped one survives.
	out = captureRun(t, "search", "retry", "--project", "tripto", "--json")
	if strings.Contains(out, "gateway retry design") || !strings.Contains(out, "tripto retry checklist") {
		t.Fatalf("query + --project should scope to the project: %s", out)
	}
}

// `nt edit --project` sets the project: frontmatter after creation (the repair
// path for a note mis-scoped at capture); 'none' clears it.
func TestEditProjectSetAndClear(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "pool exhaustion lesson", "--kind", "lesson", "--tag", "db", "--description", "d")

	captureRun(t, "edit", "pool-exhaustion-lesson", "--project", "webhookd")
	var got struct {
		Notes []map[string]any `json:"notes"`
	}
	out := captureRun(t, "index", "--project", "webhookd", "--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 {
		t.Fatalf("edited-in project should be filterable, got %d notes: %s", len(got.Notes), out)
	}

	captureRun(t, "edit", "pool-exhaustion-lesson", "--project", "none")
	out = captureRun(t, "index", "--project", "webhookd", "--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 0 {
		t.Fatalf("--project none should clear the project, still got %d notes: %s", len(got.Notes), out)
	}
}

// `nt tags` lists the project vocabulary alongside tags — "check the store's
// vocabulary before scoping" was unactionable while it showed only tags.
// The default --json stays [{tag,count}] (an existing machine contract);
// --projects lists projects alone.
func TestTagsListsProjects(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "scoped note", "--project", "WebhookD", "--tag", "design", "--description", "d")
	captureRun(t, "add", "scoped task", "--project", "webhookd")

	human := captureRun(t, "tags")
	if !strings.Contains(human, "projects:") || !strings.Contains(human, "+webhookd 2") {
		t.Fatalf("tags should list the case-folded project vocabulary with counts:\n%s", human)
	}

	// Default JSON contract unchanged: a flat [{tag,count}] array.
	var tagRows []tagCount
	if err := json.Unmarshal([]byte(captureRun(t, "tags", "--json")), &tagRows); err != nil {
		t.Fatalf("tags --json must stay [{tag,count}]: %v", err)
	}

	var projRows []projectCount
	if err := json.Unmarshal([]byte(captureRun(t, "tags", "--projects", "--json")), &projRows); err != nil {
		t.Fatal(err)
	}
	if len(projRows) != 1 || projRows[0].Project != "webhookd" || projRows[0].Count != 2 {
		t.Fatalf("tags --projects --json should fold note+task projects to one row, got %+v", projRows)
	}
}

// The unscoped `nt index` (the session-start read) warns on stderr once the
// store accumulates enough near-duplicate pairs to degrade recall — doctor,
// distill and gc are otherwise invisible until someone suspects a problem.
func TestIndexHygieneWarning(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	pairs := [][2]string{
		{"postgres pool exhaustion issue", "postgres pool exhaustion problem"},
		{"vitest mocks leak between files", "vitest mocks leak across files"},
		{"gateway retry backoff design", "gateway retry backoff approach"},
	}
	for i, p := range pairs {
		tag := "topic" + string(rune('a'+i))
		captureRun(t, "note", p[0], "--tag", tag, "--description", "d")
		captureRun(t, "note", p[1], "--tag", tag, "--force", "--description", "d")
	}

	_, stderr, code := runCaptureStderr("index")
	if code != 0 {
		t.Fatalf("index exited %d", code)
	}
	if !strings.Contains(stderr, "store hygiene") || !strings.Contains(stderr, "near-duplicate") {
		t.Fatalf("unscoped index should warn about near-duplicate pairs, stderr: %q", stderr)
	}

	// Scoped calls stay quiet — the caller is mid-task.
	_, stderr, _ = runCaptureStderr("index", "--tag", "topica")
	if strings.Contains(stderr, "store hygiene") {
		t.Fatalf("scoped index must not nag, stderr: %q", stderr)
	}
}
