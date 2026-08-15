package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nt_index gains the hard project filter the CLI has had — the MCP surface
// previously offered no project-scoped catalog at all (strict args made the
// attempt a hard error). Case-insensitive, like every project comparison.
func TestMCPIndexProjectFilter(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "webhookd retry design", "project": "WebhookD", "tags": []any{"design"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatch("nt_note", map[string]any{"title": "unrelated note", "tags": []any{"misc"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatch("nt_add", map[string]any{"text": "ship webhookd v2 +webhookd"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatch("nt_add", map[string]any{"text": "unrelated task"}); err != nil {
		t.Fatal(err)
	}

	out, err := s.dispatch("nt_index", map[string]any{"project": "webhookd"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Notes []noteStub `json:"notes"`
		Tasks []taskOut  `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 1 || res.Notes[0].Title != "webhookd retry design" {
		t.Fatalf("project filter should keep only the scoped note (case-insensitive): %s", out)
	}
	if len(res.Tasks) != 1 || !strings.Contains(res.Tasks[0].Text, "webhookd v2") {
		t.Fatalf("project filter should scope tasks by +project too: %s", out)
	}

	// A project matching no notes warns (naming mismatch), never errors — tasks
	// may still match.
	out, err = s.dispatch("nt_index", map[string]any{"project": "nosuch"})
	if err != nil {
		t.Fatal(err)
	}
	var warned struct {
		Warning string `json:"warning"`
	}
	json.Unmarshal([]byte(out), &warned)
	if !strings.Contains(warned.Warning, "no notes carry project") {
		t.Fatalf("empty project match should carry a naming-mismatch warning: %s", out)
	}
}

// nt_search accepts project as a first-class filter — and alone, since
// frontmatter is invisible to the text match, so there is no query that finds
// a project's notes otherwise.
func TestMCPSearchProjectFilter(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "gateway retry design", "project": "webhookd", "tags": []any{"design"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatch("nt_note", map[string]any{"title": "tripto retry design", "project": "tripto", "tags": []any{"design"}, "force": true}); err != nil {
		t.Fatal(err)
	}

	out, err := s.dispatch("nt_search", map[string]any{"project": "WebhookD"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gateway retry design") || strings.Contains(out, "tripto retry design") {
		t.Fatalf("bare project search should return only the scoped note: %s", out)
	}

	if _, err := s.dispatch("nt_search", map[string]any{}); err == nil {
		t.Fatal("nt_search with no query/tag/project must be rejected")
	}
}

// nt_note_edit can set and clear project: — the repair path for a note
// mis-scoped at capture, without a supersede.
func TestMCPNoteEditProject(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "pool exhaustion lesson", "kind": "lesson", "tags": []any{"db"}})
	if err != nil {
		t.Fatal(err)
	}
	var created noteOut
	json.Unmarshal([]byte(out), &created)

	if _, err := s.dispatch("nt_note_edit", map[string]any{"id": created.ID, "project": "webhookd"}); err != nil {
		t.Fatal(err)
	}
	scoped, err := s.dispatch("nt_index", map[string]any{"project": "webhookd"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, "pool exhaustion lesson") {
		t.Fatalf("edited-in project should be filterable: %s", scoped)
	}

	if _, err := s.dispatch("nt_note_edit", map[string]any{"id": created.ID, "project": "none"}); err != nil {
		t.Fatal(err)
	}
	scoped, err = s.dispatch("nt_index", map[string]any{"project": "webhookd"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scoped, "pool exhaustion lesson") {
		t.Fatalf(`project "none" should clear the scope: %s`, scoped)
	}
}

// The unscoped nt_index (the session-start read) carries a hygiene warning
// once near-duplicate pairs cross the shared threshold — same nag, same
// number, as the CLI.
func TestMCPIndexHygieneWarning(t *testing.T) {
	s := newServer(t)
	pairs := [][2]string{
		{"postgres pool exhaustion issue", "postgres pool exhaustion problem"},
		{"vitest mocks leak between files", "vitest mocks leak across files"},
		{"gateway retry backoff design", "gateway retry backoff approach"},
	}
	for i, p := range pairs {
		tag := "topic" + string(rune('a'+i))
		if _, err := s.dispatch("nt_note", map[string]any{"title": p[0], "tags": []any{tag}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.dispatch("nt_note", map[string]any{"title": p[1], "tags": []any{tag}, "force": true}); err != nil {
			t.Fatal(err)
		}
	}

	out, err := s.dispatch("nt_index", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Warning string `json:"warning"`
	}
	json.Unmarshal([]byte(out), &res)
	if !strings.Contains(res.Warning, "near-duplicate") || !strings.Contains(res.Warning, "nt_distill") {
		t.Fatalf("unscoped nt_index should warn toward nt_distill once pairs accumulate: %s", out)
	}

	// Scoped calls stay quiet.
	out, err = s.dispatch("nt_index", map[string]any{"tag": "topica"})
	if err != nil {
		t.Fatal(err)
	}
	res.Warning = ""
	json.Unmarshal([]byte(out), &res)
	if strings.Contains(res.Warning, "near-duplicate") {
		t.Fatalf("scoped nt_index must not nag: %s", out)
	}
}

// Config [decay] kind defaults reach MCP captures too — nt_note kind:"lesson"
// stamps the configured half_life when the caller gives none.
func TestMCPNoteKindDecayDefault(t *testing.T) {
	s := newServer(t)
	cfg := "[decay]\nlesson = \"90d\"\n"
	if err := os.WriteFile(filepath.Join(s.eng.S.Dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := s.dispatch("nt_note", map[string]any{"title": "vitest mocks leak", "kind": "lesson", "tags": []any{"vitest"}})
	if err != nil {
		t.Fatal(err)
	}
	var created noteOut
	json.Unmarshal([]byte(out), &created)
	data, err := os.ReadFile(filepath.Join(s.eng.S.Dir, "notes", "lessons", "vitest-mocks-leak.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "half_life: 90d") {
		t.Fatalf("kind default should stamp half_life into frontmatter:\n%s", data)
	}

	// Explicit half_life wins.
	if _, err := s.dispatch("nt_note", map[string]any{"title": "another lesson here", "kind": "lesson", "tags": []any{"z"}, "half_life": "30d"}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(s.eng.S.Dir, "notes", "lessons", "another-lesson-here.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "half_life: 30d") {
		t.Fatalf("explicit half_life should win over the config default:\n%s", data)
	}
}
