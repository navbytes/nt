package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRecallIncludesNoteBody: recall --json must carry the note body, so a
// resuming agent recovers the finding itself, not just its title (Product #4).
func TestRecallIncludesNoteBody(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "JWT expiry",
		"--body", "Root cause in auth.go:42; refresh window is 7d.",
		"--source", "claude", "--tag", "auth")

	var out struct {
		Notes []noteJSON `json:"notes"`
	}
	if err := json.Unmarshal([]byte(captureRun(t, "recall", "--source", "claude", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(out.Notes))
	}
	if !strings.Contains(out.Notes[0].Body, "auth.go:42") {
		t.Fatalf("recall --json must include the note body, got %q", out.Notes[0].Body)
	}
}
