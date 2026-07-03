package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkstreamAutoSentinel: NT_WORKSTREAM=auto resolves via derivation, and the
// explicit "auto" arg does too.
func TestWorkstreamAutoSentinel(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	want := filepath.Base(dir)
	s := &server{}
	t.Setenv("NT_WORKSTREAM", "auto")
	if got := s.workstream(map[string]any{}); got != want {
		t.Errorf("env auto = %q, want %q", got, want)
	}
	t.Setenv("NT_WORKSTREAM", "")
	if got := s.workstream(map[string]any{"workstream": "auto"}); got != want {
		t.Errorf("arg auto = %q, want %q", got, want)
	}
}

// TestMCPWorkstreamArgNormalized: an explicit workstream arg with spaces stamps
// (and reads back as) the normalized dashed id — a raw space would truncate the
// stamp on the space-delimited todo.txt line and hide the task from its creator.
func TestMCPWorkstreamArgNormalized(t *testing.T) {
	s := newServer(t)
	t.Setenv("NT_WORKSTREAM", "")
	out, err := s.dispatch("nt_add", map[string]any{"text": "spacey scoped task", "workstream": "my stream"})
	if err != nil {
		t.Fatal(err)
	}
	var added taskOut
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if added.Workstream != "my-stream" || added.Text != "spacey scoped task" {
		t.Fatalf("workstream arg should normalize to my-stream with clean text, got %+v", added)
	}
	// A reader under the same spacey identity sees it (both normalize to my-stream).
	sout, _ := s.dispatch("nt_status", map[string]any{"workstream": "my stream"})
	if !strings.Contains(sout, "spacey scoped task") {
		t.Fatalf(`reader under "my stream" should see its own task: %s`, sout)
	}
	// nt_update's explicit reassign normalizes too.
	uout, err := s.dispatch("nt_update", map[string]any{"id": added.ID, "workstream": "an other"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uout, `"workstream":"an-other"`) {
		t.Fatalf("nt_update reassign should normalize the workstream: %s", uout)
	}
}
