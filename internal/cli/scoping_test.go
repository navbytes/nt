package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// CLI reads scope to NT_WORKSTREAM (symmetric with the MCP tools): a task stamped
// by another workstream is hidden; unstamped/shared tasks stay visible; --workstream
// '*' widens.
func TestReadScopingByWorkstream(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)

	t.Setenv("NT_WORKSTREAM", "alice")
	captureRun(t, "add", "alice task")
	t.Setenv("NT_WORKSTREAM", "bob")
	captureRun(t, "add", "bob task")
	t.Setenv("NT_WORKSTREAM", "")
	captureRun(t, "add", "shared task")

	// alice's ready sees her own + shared, not bob's.
	t.Setenv("NT_WORKSTREAM", "alice")
	got := captureRun(t, "ready", "--json")
	if !strings.Contains(got, "alice task") || !strings.Contains(got, "shared task") {
		t.Errorf("alice should see her own + shared: %s", got)
	}
	if strings.Contains(got, "bob task") {
		t.Errorf("alice must not see bob's task: %s", got)
	}
	// --workstream '*' widens to all.
	all := captureRun(t, "ready", "--workstream", "*", "--json")
	if !strings.Contains(all, "bob task") {
		t.Errorf("--workstream '*' should show all: %s", all)
	}
	// list scopes the same way.
	l := captureRun(t, "list", "--json")
	if strings.Contains(l, "bob task") {
		t.Errorf("list should scope to alice too: %s", l)
	}
}

// nt index --updated-since filters by change date; a future date returns nothing.
func TestIndexUpdatedSince(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Recent note", "--folder", "ref", "--description", "d", "--body", "b")

	var today, future struct {
		Notes []map[string]any `json:"notes"`
	}
	json.Unmarshal([]byte(captureRun(t, "index", "--updated-since", "today", "--json")), &today)
	if len(today.Notes) != 1 {
		t.Fatalf("--updated-since today should include today's note, got %d", len(today.Notes))
	}
	json.Unmarshal([]byte(captureRun(t, "index", "--updated-since", "+1d", "--json")), &future)
	if len(future.Notes) != 0 {
		t.Fatalf("--updated-since tomorrow should exclude everything, got %d", len(future.Notes))
	}
}

// nt index stubs carry the source (author) for ownership on a shared store.
func TestIndexStubSource(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Authored", "--folder", "ref", "--source", "dana", "--description", "d", "--body", "b")
	out := captureRun(t, "index", "--json")
	if !strings.Contains(out, `"source": "dana"`) {
		t.Errorf("index stub should include source: %s", out)
	}
}

// nt relink rewrites a wrong outbound [[link]] in a note body.
func TestRelink(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Real target", "--folder", "ref", "--tag", "z", "--body", "x")
	captureRun(t, "note", "Has bad link", "--folder", "ref", "--tag", "z", "--force", "--body", "see [[wrong]]")

	// relinking to a nonexistent target is refused.
	if _, code := runWithStdout("relink", "has-bad-link", "wrong", "does-not-exist"); code == 0 {
		t.Error("relink to an unresolved target should fail")
	}
	// relink to the real note succeeds and rewrites the body.
	out := captureRun(t, "relink", "has-bad-link", "wrong", "real-target")
	if !strings.Contains(out, "relinked 1") {
		t.Errorf("relink should report 1 rewrite: %s", out)
	}
	if body := captureRun(t, "show", "has-bad-link"); !strings.Contains(body, "[[real-target]]") || strings.Contains(body, "[[wrong]]") {
		t.Errorf("body should now link real-target: %s", body)
	}
}
