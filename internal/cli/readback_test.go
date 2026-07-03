package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// Read-back fidelity fixes from the multi-agent field study: descriptions are
// first-class content, show opens tasks, index surfaces blocked work, and
// log/review honor the workstream scope like list/ready.

func TestShowPrintsDescriptionAndJSONCarriesIt(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	// A stub-style lesson: ALL content in the description, empty body — the
	// pattern that used to be unreadable through the CLI front door.
	captureRun(t, "note", "pytest is not preinstalled", "--lesson",
		"--description", "run pip install pytest first; PYTHONPATH=. is required for the tests to import the package")

	out := captureRun(t, "show", "pytest is not preinstalled")
	if !strings.Contains(out, "PYTHONPATH=.") {
		t.Fatalf("show must print the description of a body-less note:\n%s", out)
	}
	var j struct {
		Description string `json:"description"`
	}
	json.Unmarshal([]byte(captureRun(t, "show", "pytest is not preinstalled", "--json")), &j)
	if !strings.Contains(j.Description, "pip install pytest") {
		t.Fatalf("show --json must carry the untruncated description, got %q", j.Description)
	}
	// And export must not produce an empty section for it.
	exp := captureRun(t, "export", "--tag", "lesson")
	if !strings.Contains(exp, "PYTHONPATH=.") {
		t.Fatalf("export folded description missing:\n%s", exp)
	}
}

func TestShowOpensTasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "implement per-key limiter", "--pri", "high", "--body", "LRU of buckets keyed by client id")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)

	out := captureRun(t, "show", listed[0].ID)
	if !strings.Contains(out, "implement per-key limiter") || !strings.Contains(out, "LRU of buckets") {
		t.Fatalf("show <task-id> should render the task and inline its detail note:\n%s", out)
	}
}

func TestIndexListsBlockedTasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "custom aliases")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	captureRun(t, "add", "hit counter", "--blocked-by", listed[0].ID)

	out := captureRun(t, "index")
	if !strings.Contains(out, "Blocked") || !strings.Contains(out, "hit counter") {
		t.Fatalf("index should surface blocked tasks as stubs:\n%s", out)
	}
	var j struct {
		Blocked []struct {
			Text string `json:"text"`
		} `json:"blocked"`
		Notes []any `json:"notes"`
	}
	json.Unmarshal([]byte(captureRun(t, "index", "--json")), &j)
	if len(j.Blocked) != 1 {
		t.Fatalf("index --json should carry blocked tasks, got %+v", j)
	}
	if j.Notes == nil {
		t.Fatal("index --json notes must be [], not null")
	}
}

func TestLogAndReviewScopeToWorkstream(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_WORKSTREAM", "mine")
	captureRun(t, "add", "my finished thing")
	var mine []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &mine)
	captureRun(t, "done", mine[0].ID)

	t.Setenv("NT_WORKSTREAM", "theirs")
	captureRun(t, "add", "their finished thing", "--due", "2020-01-01") // overdue for review
	var theirs []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &theirs)
	captureRun(t, "done", theirs[0].ID)

	t.Setenv("NT_WORKSTREAM", "mine")
	out := captureRun(t, "log")
	if strings.Contains(out, "their finished thing") {
		t.Fatalf("log should scope to NT_WORKSTREAM:\n%s", out)
	}
	if !strings.Contains(out, "my finished thing") {
		t.Fatalf("log lost the caller's own completions:\n%s", out)
	}
	// "*" widens.
	out = captureRun(t, "log", "--workstream", "*")
	if !strings.Contains(out, "their finished thing") {
		t.Fatalf("log --workstream '*' should show all:\n%s", out)
	}

	// review: another workstream's overdue task is not my triage.
	t.Setenv("NT_WORKSTREAM", "theirs")
	captureRun(t, "add", "their overdue task", "--due", "2020-01-01")
	t.Setenv("NT_WORKSTREAM", "mine")
	out = captureRun(t, "review")
	if strings.Contains(out, "their overdue task") {
		t.Fatalf("review should scope to NT_WORKSTREAM:\n%s", out)
	}
}

func TestDoctorOrphanExemptions(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "single-flight the refresh", "--lesson", "--description", "x")
	captureRun(t, "note", "chose flock over sqlite", "--folder", "decisions", "--description", "y")
	captureRun(t, "note", "an actually stray note", "--description", "z", "--body", "stray")

	out := captureRun(t, "doctor", "--check")
	if strings.Contains(out, "single-flight") || strings.Contains(out, "flock") {
		t.Fatalf("lessons/decisions must not be counted as orphans:\n%s", out)
	}
	if !strings.Contains(out, "1 orphan(s)") {
		t.Fatalf("the genuinely stray note should still be flagged:\n%s", out)
	}
}

func TestReadyAnnotatesOpenSubtasks(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "implement aliases")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	captureRun(t, "add", "decide collision contract", "--parent", listed[0].ID)

	out := captureRun(t, "ready")
	if !strings.Contains(out, "open subtask") {
		t.Fatalf("ready should warn that the parent has open subtasks:\n%s", out)
	}
}

func TestUpdateBodyAppendsToExistingDetailNote(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "task with detail", "--body", "first detail")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	// Second --body must APPEND to the existing detail note, not mint slug-2.md.
	captureRun(t, "update", listed[0].ID, "--body", "second detail")
	shown := captureRun(t, "show", listed[0].ID)
	if !strings.Contains(shown, "first detail") || !strings.Contains(shown, "second detail") {
		t.Fatalf("update --body should append to the existing detail note:\n%s", shown)
	}
	var notes []struct {
		Title string `json:"title"`
	}
	json.Unmarshal([]byte(captureRun(t, "notes", "--json")), &notes)
	for _, n := range notes {
		if strings.HasSuffix(n.Title, "-2") {
			t.Fatalf("a duplicate detail note was created: %q", n.Title)
		}
	}
}

func TestEditDescNonInteractive(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "needs a summary", "--body", "long body here")
	captureRun(t, "edit", "needs a summary", "--desc", "the one-line summary")
	var j struct {
		Description string `json:"description"`
	}
	json.Unmarshal([]byte(captureRun(t, "show", "needs a summary", "--json")), &j)
	if j.Description != "the one-line summary" {
		t.Fatalf("edit --desc failed, got %q", j.Description)
	}
	// Replacing an existing description keeps a single frontmatter line.
	captureRun(t, "edit", "needs a summary", "--desc", "revised summary")
	json.Unmarshal([]byte(captureRun(t, "show", "needs a summary", "--json")), &j)
	if j.Description != "revised summary" {
		t.Fatalf("edit --desc replace failed, got %q", j.Description)
	}
}
