package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/mutate"
)

func newServer(t *testing.T) *server {
	t.Setenv("NT_DIR", t.TempDir())
	e, err := mutate.Open()
	if err != nil {
		t.Fatal(err)
	}
	return &server{eng: e, version: "test"}
}

func TestMCPToolFlow(t *testing.T) {
	s := newServer(t)

	// nt_add: defaults source to claude, folds tags into the text.
	out, err := s.dispatch("nt_add", map[string]any{"text": "fix race", "priority": "high", "tags": []any{"auth"}})
	if err != nil {
		t.Fatal(err)
	}
	var added taskOut
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if added.Text != "fix race @auth" || added.Priority != "A" || added.Source != "claude" {
		t.Fatalf("nt_add result wrong: %+v", added)
	}

	// nt_ready surfaces it.
	out, _ = s.dispatch("nt_ready", map[string]any{})
	var ready []taskOut
	json.Unmarshal([]byte(out), &ready)
	if len(ready) != 1 || ready[0].ID != added.ID {
		t.Fatalf("nt_ready should return the new task, got %+v", ready)
	}

	// nt_done by stable id removes it from ready; nt_log shows it.
	if _, err := s.dispatch("nt_done", map[string]any{"id": added.ID}); err != nil {
		t.Fatal(err)
	}
	out, _ = s.dispatch("nt_ready", map[string]any{})
	json.Unmarshal([]byte(out), &ready)
	if len(ready) != 0 {
		t.Errorf("nt_ready should be empty after done, got %d", len(ready))
	}
	out, _ = s.dispatch("nt_log", map[string]any{})
	var logged []taskOut
	json.Unmarshal([]byte(out), &logged)
	if len(logged) != 1 {
		t.Errorf("nt_log should list the completed task, got %d", len(logged))
	}

	// Positional handles are refused (agents must use stable ids).
	if _, err := s.dispatch("nt_done", map[string]any{"id": "task:1"}); err == nil {
		t.Error("nt_done must reject a positional handle")
	}

	// nt_note + nt_recall round-trips the body.
	if _, err := s.dispatch("nt_note", map[string]any{"title": "JWT", "body": "root cause auth.go:42"}); err != nil {
		t.Fatal(err)
	}
	out, _ = s.dispatch("nt_recall", map[string]any{})
	var rec struct {
		Notes []noteOut `json:"notes"`
	}
	json.Unmarshal([]byte(out), &rec)
	if len(rec.Notes) != 1 || !strings.Contains(rec.Notes[0].Body, "auth.go:42") {
		t.Errorf("nt_recall should include the note body, got %+v", rec.Notes)
	}
}

func TestMCPAddSplitsLongCapture(t *testing.T) {
	s := newServer(t)
	long := "Investigate and resolve the intermittent 500 errors during peak traffic by " +
		"correlating traces across the gateway and token service, validating the connection " +
		"pool exhaustion hypothesis, and coordinating with the infra team on a scaling change."

	out, err := s.dispatch("nt_add", map[string]any{"text": long, "project": "ops"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Task taskOut `json:"task"`
		Note string  `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Note == "" {
		t.Fatalf("a paragraph-length capture should create a linked note; got %s", out)
	}
	if n := len([]rune(res.Task.Text)); n > 90 {
		t.Errorf("task text should be a short line, got %d runes: %q", n, res.Task.Text)
	}
	if !strings.Contains(res.Task.Text, "[[") {
		t.Errorf("the split task should link its detail note: %q", res.Task.Text)
	}
	// The full text is preserved in the note body (retrievable via recall).
	rout, _ := s.dispatch("nt_recall", map[string]any{})
	if !strings.Contains(rout, "connection pool exhaustion") {
		t.Error("the note should hold the full original text")
	}

	// A normal short capture is untouched (no note, no link).
	out2, _ := s.dispatch("nt_add", map[string]any{"text": "buy milk"})
	if strings.Contains(out2, "\"note\"") {
		t.Errorf("a short task should not split: %s", out2)
	}
}

func TestMCPStatus(t *testing.T) {
	s := newServer(t)
	// A note + two tasks in +auth (one linked to the note), one marked doing.
	if _, err := s.dispatch("nt_note", map[string]any{"title": "Auth Design", "body": "the plan", "tags": []any{"auth"}}); err != nil {
		t.Fatal(err)
	}
	s.dispatch("nt_add", map[string]any{"text": "ship auth [[Auth Design]]", "project": "auth"})
	s.dispatch("nt_add", map[string]any{"text": "review auth", "project": "auth"})
	s.dispatch("nt_add", map[string]any{"text": "buy milk"}) // out of scope

	out, _ := s.dispatch("nt_ready", map[string]any{"project": "auth"})
	var ready []taskOut
	json.Unmarshal([]byte(out), &ready)
	if len(ready) == 0 {
		t.Fatal("seeded +auth tasks should be ready")
	}
	s.dispatch("nt_update", map[string]any{"id": ready[0].ID, "status": "doing"})

	out, err := s.dispatch("nt_status", map[string]any{"project": "auth"})
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Scope       string              `json:"scope"`
		Counts      map[string]int      `json:"counts"`
		Doing       []taskOut           `json:"doing"`
		LinkedNotes []map[string]string `json:"linkedNotes"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	if st.Scope != "+auth" {
		t.Errorf("scope = %q, want +auth", st.Scope)
	}
	if len(st.Doing) != 1 {
		t.Errorf("expected 1 doing task, got %d", len(st.Doing))
	}
	if st.Counts["open"] < 1 {
		t.Errorf("expected open tasks in +auth, got counts %v", st.Counts)
	}
	found := false
	for _, n := range st.LinkedNotes {
		if n["title"] == "Auth Design" {
			found = true
		}
	}
	if !found {
		t.Errorf("the [[Auth Design]] note should surface as a linked note, got %+v", st.LinkedNotes)
	}
}

func TestMCPProtocol(t *testing.T) {
	s := newServer(t)

	// A notification (no id) gets no reply.
	if _, reply := s.handle(request{Method: "notifications/initialized"}); reply {
		t.Error("notifications must not get a reply")
	}
	// initialize succeeds.
	if resp, reply := s.handle(request{ID: json.RawMessage("1"), Method: "initialize"}); !reply || resp.Error != nil {
		t.Fatalf("initialize failed: %+v", resp)
	}
	// tools/list advertises every tool.
	resp, _ := s.handle(request{ID: json.RawMessage("2"), Method: "tools/list"})
	tools := resp.Result.(map[string]any)["tools"].([]toolDef)
	if len(tools) != len(toolDefs) || len(tools) == 0 {
		t.Errorf("tools/list should advertise %d tools, got %d", len(toolDefs), len(tools))
	}
	// Unknown method → JSON-RPC error.
	if resp, _ := s.handle(request{ID: json.RawMessage("3"), Method: "bogus"}); resp.Error == nil {
		t.Error("unknown method should return an error")
	}
}

func TestMCPRetrievalTools(t *testing.T) {
	s := newServer(t)
	mustDispatch := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	mustDispatch("nt_note", map[string]any{"title": "Auth Design", "folder": "ref", "tags": []any{"auth"}, "body": "see [[token-rotation]]"})
	mustDispatch("nt_note", map[string]any{"title": "Token Rotation", "folder": "ref", "tags": []any{"auth"}, "body": "rotate weekly"})
	mustDispatch("nt_add", map[string]any{"text": "implement [[auth-design]]", "tags": []any{"auth"}})

	// nt_search by tag → both notes; by query → the matching one.
	var sr struct {
		Notes []noteOut `json:"notes"`
	}
	json.Unmarshal([]byte(mustDispatch("nt_search", map[string]any{"tag": "auth", "type": "note"})), &sr)
	if len(sr.Notes) != 2 {
		t.Fatalf("nt_search tag=auth → %d notes, want 2", len(sr.Notes))
	}
	if out := mustDispatch("nt_search", map[string]any{"query": "rotate"}); !strings.Contains(out, "Token Rotation") {
		t.Fatalf("nt_search query missed the note: %s", out)
	}
	if _, err := s.dispatch("nt_search", map[string]any{}); err == nil {
		t.Error("nt_search should require query or tag")
	}

	// nt_links: forward (note) + backlink (task).
	if out := mustDispatch("nt_links", map[string]any{"handle": "auth-design"}); !strings.Contains(out, "Token Rotation") || !strings.Contains(out, "implement [[auth-design]]") {
		t.Fatalf("nt_links missing forward/backlink: %s", out)
	}

	// nt_mv: refile, and dest is required.
	if out := mustDispatch("nt_mv", map[string]any{"handle": "auth-design", "dest": "archive/auth-design"}); !strings.Contains(out, "archive/auth-design.md") {
		t.Fatalf("nt_mv result: %s", out)
	}
	if _, err := s.dispatch("nt_mv", map[string]any{"handle": "x"}); err == nil {
		t.Error("nt_mv should require dest")
	}

	// nt_tag: add/remove on a note, preserving the rest.
	if out := mustDispatch("nt_tag", map[string]any{"handle": "token-rotation", "add": []any{"ref"}, "remove": []any{"auth"}}); !strings.Contains(out, "ref") || strings.Contains(out, "auth") {
		t.Fatalf("nt_tag result: %s", out)
	}
}
