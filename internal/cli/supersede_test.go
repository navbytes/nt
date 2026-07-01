package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// The dedup-on-write guard refuses a near-duplicate note; --force overrides;
// --supersede replaces and retires the old note from the index.
func TestNoteDedupGuardForceAndSupersede(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Token storage in httpOnly cookie", "--folder", "decisions", "--tag", "auth", "--description", "cookie")

	// Near-duplicate (shares tag auth + heavy title overlap) is refused.
	if out, code := runWithStdout("note", "Token storage cookie approach", "--tag", "auth"); code == 0 {
		t.Fatalf("near-duplicate should be refused:\n%s", out)
	}

	// --force creates it anyway → 2 notes in the index.
	captureRun(t, "note", "Token storage cookie approach", "--tag", "auth", "--force", "--description", "fork")
	var idx struct {
		Notes []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"notes"`
	}
	json.Unmarshal([]byte(captureRun(t, "index", "--json")), &idx)
	if len(idx.Notes) != 2 {
		t.Fatalf("expected 2 notes after --force, got %d", len(idx.Notes))
	}

	// Supersede the fork with a new canonical note; the old fork leaves the index.
	captureRun(t, "note", "Token storage: final decision", "--tag", "auth", "--supersede", idx.Notes[0].ID, "--description", "canonical")
	json.Unmarshal([]byte(captureRun(t, "index", "--json")), &idx)
	for _, n := range idx.Notes {
		if n.ID == "" {
			continue
		}
	}
	titles := ""
	for _, n := range idx.Notes {
		titles += n.Title + "|"
	}
	if strings.Contains(titles, "cookie approach") {
		t.Errorf("superseded note should be gone from the index, got: %s", titles)
	}
	if !strings.Contains(titles, "final decision") {
		t.Errorf("canonical note should be in the index, got: %s", titles)
	}
}

// nt supersede <old> --by <new> retires the old note.
func TestSupersedeCommand(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Old schema decision", "--folder", "decisions", "--tag", "db", "--description", "old")
	captureRun(t, "note", "New schema decision", "--folder", "decisions", "--tag", "db", "--force", "--description", "new")

	out := captureRun(t, "supersede", "old-schema-decision", "--by", "new-schema-decision")
	if !strings.Contains(out, "canonical") {
		t.Errorf("supersede should confirm: %s", out)
	}
	idx := captureRun(t, "index", "--json")
	if strings.Contains(idx, "Old schema decision") {
		t.Errorf("old note should be retired from index: %s", idx)
	}
	if !strings.Contains(idx, "New schema decision") {
		t.Errorf("new note should remain: %s", idx)
	}
}
