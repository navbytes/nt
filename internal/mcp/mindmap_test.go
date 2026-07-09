package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPMindmap(t *testing.T) {
	s := newServer(t)

	res, err := s.dispatch("nt_note", map[string]any{
		"title": "Project Alpha",
		"body":  "## Goals\n- Ship\n  - Auth\n## Risks\n- Leakage\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	var created noteOut
	if err := json.Unmarshal([]byte(res), &created); err != nil {
		t.Fatalf("decode note: %v", err)
	}

	// Fenced mindmap for the note's outline.
	out, err := s.dispatch("nt_mindmap", map[string]any{"handle": created.ID})
	if err != nil {
		t.Fatalf("nt_mindmap: %v", err)
	}
	// Structural assertions (the root label follows nt's own title derivation,
	// which we don't re-test here).
	for _, want := range []string{"```mermaid", "mindmap", "root((", "Goals", "Auth", "Risks"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mindmap output missing %q:\n%s", want, out)
		}
	}
	_ = created.Title

	// depth=1 stops at headings; no_fence drops the code fence.
	raw, err := s.dispatch("nt_mindmap", map[string]any{"handle": created.ID, "depth": float64(1), "no_fence": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "```") {
		t.Fatalf("no_fence should omit the code fence:\n%s", raw)
	}
	if strings.Contains(raw, "Auth") {
		t.Fatalf("depth 1 should stop before list items:\n%s", raw)
	}

	// A note with no headings/lists is an error, not an empty diagram.
	empty, _ := s.dispatch("nt_note", map[string]any{"title": "Flat", "body": "just a paragraph"})
	var flat noteOut
	if err := json.Unmarshal([]byte(empty), &flat); err != nil {
		t.Fatalf("decode note: %v", err)
	}
	if _, err := s.dispatch("nt_mindmap", map[string]any{"handle": flat.ID}); err == nil {
		t.Fatal("expected an error for a note with nothing to map")
	}
}
