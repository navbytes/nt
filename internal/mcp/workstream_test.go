package mcp

import (
	"path/filepath"
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
