package mcp

import (
	"strings"
	"testing"
)

// A misnamed parameter must fail loudly, not silently vanish (an agent passing
// content: instead of body: used to get an empty note with no warning).
func TestMCPRejectsUnknownParams(t *testing.T) {
	s := newServer(t)
	_, err := s.dispatch("nt_note", map[string]any{"title": "x y z", "content": "the whole finding"})
	if err == nil {
		t.Fatal("unknown parameter should be rejected")
	}
	if !strings.Contains(err.Error(), `"content"`) || !strings.Contains(err.Error(), "body") {
		t.Fatalf("error should name the bad param and suggest the real one: %v", err)
	}
	// Known params still pass.
	if _, err := s.dispatch("nt_note", map[string]any{"title": "x y z", "body": "ok"}); err != nil {
		t.Fatalf("valid call refused: %v", err)
	}
}

// nt_rm on a note id should redirect to nt_archive, not report a bare "no task".
func TestMCPRmNoteIDRedirect(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "a note not a task"})
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, part := range strings.Split(out, `"`) {
		if len(part) == 26 && strings.HasPrefix(part, "01") { // ULID
			id = part
			break
		}
	}
	if id == "" {
		t.Fatalf("no note id found in %s", out)
	}
	_, err = s.dispatch("nt_rm", map[string]any{"id": id})
	if err == nil || !strings.Contains(err.Error(), "nt_archive") {
		t.Fatalf("nt_rm on a note id should redirect to nt_archive, got: %v", err)
	}
}
