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
