package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/view"
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

// TestMCPArchive: an agent can retire stale memory (and restore it). Archived
// notes drop out of search/recall and out of backlinks, but the note is still
// addressable by handle (so undo works).
func TestMCPArchive(t *testing.T) {
	s := newServer(t)
	must := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	must("nt_note", map[string]any{"title": "Keeper", "tags": []any{"keep"}, "body": "the durable decision"})
	must("nt_note", map[string]any{"title": "Stale Note", "tags": []any{"keep"}, "body": "obsoletemarker — points at [[keeper]]"})

	searchKeep := func() int {
		var sr struct {
			Notes []noteOut `json:"notes"`
		}
		json.Unmarshal([]byte(must("nt_search", map[string]any{"tag": "keep", "type": "note"})), &sr)
		return len(sr.Notes)
	}

	// Baseline: both notes searchable; Keeper carries a backlink from Stale Note.
	if n := searchKeep(); n != 2 {
		t.Fatalf("baseline search tag=keep → %d, want 2", n)
	}
	if out := must("nt_links", map[string]any{"handle": "keeper"}); !strings.Contains(out, "Stale Note") {
		t.Fatalf("baseline nt_links keeper should show the backlink: %s", out)
	}

	// Archive Stale Note.
	var ar noteOut
	json.Unmarshal([]byte(must("nt_archive", map[string]any{"handle": "stale-note"})), &ar)
	if !ar.Archived {
		t.Fatalf("nt_archive result should report archived: %+v", ar)
	}

	// It drops out of search, recall, and Keeper's backlinks.
	if n := searchKeep(); n != 1 {
		t.Errorf("after archive, search tag=keep → %d, want 1", n)
	}
	if out := must("nt_recall", map[string]any{}); strings.Contains(out, "obsoletemarker") {
		t.Error("nt_recall must not return an archived note")
	}
	if out := must("nt_links", map[string]any{"handle": "keeper"}); strings.Contains(out, "Stale Note") {
		t.Errorf("an archived note must not appear as a backlink: %s", out)
	}

	// Undo restores it everywhere. (Fresh var: the result omits archived when
	// false, so reusing `ar` would keep its stale true.)
	var un noteOut
	json.Unmarshal([]byte(must("nt_archive", map[string]any{"handle": "stale-note", "undo": true})), &un)
	if un.Archived {
		t.Error("nt_archive undo should clear the flag")
	}
	if n := searchKeep(); n != 2 {
		t.Errorf("after undo, search tag=keep → %d, want 2", n)
	}

	// Validation.
	if _, err := s.dispatch("nt_archive", map[string]any{}); err == nil {
		t.Error("nt_archive should require a handle")
	}
	if _, err := s.dispatch("nt_archive", map[string]any{"handle": "nope"}); err == nil {
		t.Error("nt_archive should error on an unknown note")
	}
}

// TestMCPView: nt_view lists the saved smart views and recalls one through the
// same view.Apply as the CLI/web — so an agent sees exactly the user's query.
func TestMCPView(t *testing.T) {
	s := newServer(t)

	// Empty store: listing works and is empty.
	out, err := s.dispatch("nt_view", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"views": []`) {
		t.Fatalf("empty views list, got %s", out)
	}

	// Seed tasks and a saved view.
	s.dispatch("nt_add", map[string]any{"text": "fix auth", "tags": []any{"backend"}})
	s.dispatch("nt_add", map[string]any{"text": "write docs", "tags": []any{"docs"}})
	if err := view.Save(s.eng.S.Dir, map[string]view.Spec{"backend": {Tag: "backend"}}); err != nil {
		t.Fatal(err)
	}

	// Listing names it with its filter summary.
	out, _ = s.dispatch("nt_view", map[string]any{})
	if !strings.Contains(out, `"name": "backend"`) || !strings.Contains(out, "--tag backend") {
		t.Fatalf("views list should name the view + filter, got %s", out)
	}

	// Recalling applies the filter.
	out, err = s.dispatch("nt_view", map[string]any{"name": "backend"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fix auth") || strings.Contains(out, "write docs") {
		t.Fatalf("view should keep @backend and drop @docs, got %s", out)
	}

	// Unknown names error helpfully.
	if _, err := s.dispatch("nt_view", map[string]any{"name": "nope"}); err == nil {
		t.Fatal("unknown view should error")
	}
}

// TestMCPAddWithBody: nt_add with a body keeps the short title on the task and
// saves the detail as a linked note filed under notes/tasks/ (so machine-made
// task notes don't clutter a human's folders).
func TestMCPAddWithBody(t *testing.T) {
	s := newServer(t)

	out, err := s.dispatch("nt_add", map[string]any{
		"text": "Fix token refresh race",
		"body": "Two requests refresh at once; add a single-flight guard keyed on the refresh-token id.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A task with a body comes back wrapped: {task, note, hint}.
	var wrapped struct {
		Task taskOut `json:"task"`
		Note string  `json:"note"`
		Hint string  `json:"hint"`
	}
	if err := json.Unmarshal([]byte(out), &wrapped); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wrapped.Task.Text, "Fix token refresh race") || !strings.Contains(wrapped.Task.Text, "[[") {
		t.Errorf("task should keep its title and link the body note, got %q", wrapped.Task.Text)
	}

	// The note exists, under notes/tasks/, with the body.
	notes, _ := note.List(s.eng.S)
	var body *note.Note
	for _, n := range notes {
		if strings.HasPrefix(n.Rel, note.TaskNoteFolder+"/") {
			body = n
		}
	}
	if body == nil {
		t.Fatalf("body note should be filed under %s/; rels=%v", note.TaskNoteFolder, relsOf(notes))
	}
	if !strings.Contains(body.Body, "single-flight guard") {
		t.Errorf("note should hold the body text, got %q", body.Body)
	}

	// A plain add (no body, short title) makes no note.
	s.dispatch("nt_add", map[string]any{"text": "buy milk"})
	notes2, _ := note.List(s.eng.S)
	if len(notes2) != len(notes) {
		t.Errorf("a short task with no body should not create a note: %d → %d", len(notes), len(notes2))
	}
}

func relsOf(ns []*note.Note) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Rel
	}
	return out
}

// TestMCPRecallContextControls: brief drops note bodies (pointers only) and
// limit caps the result — the opt-in levers for keeping recall's context small.
func TestMCPRecallContextControls(t *testing.T) {
	s := newServer(t)
	must := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	must("nt_note", map[string]any{"title": "Decision A", "folder": "decisions", "body": "long rationale here"})
	must("nt_note", map[string]any{"title": "Decision B", "folder": "decisions", "body": "more reasoning here"})

	type recallOut struct {
		Tasks []taskOut `json:"tasks"`
		Notes []noteOut `json:"notes"`
	}
	var full recallOut
	json.Unmarshal([]byte(must("nt_recall", map[string]any{})), &full)
	if len(full.Notes) != 2 || full.Notes[0].Body == "" {
		t.Fatalf("default recall should include 2 notes with bodies, got %+v", full.Notes)
	}

	var brief recallOut
	json.Unmarshal([]byte(must("nt_recall", map[string]any{"brief": true})), &brief)
	if len(brief.Notes) != 2 {
		t.Fatalf("brief recall should still list 2 notes, got %d", len(brief.Notes))
	}
	for _, n := range brief.Notes {
		if n.Body != "" {
			t.Fatalf("brief recall must omit bodies, got %q", n.Body)
		}
		if n.Title == "" {
			t.Fatal("brief recall should still carry titles (pointers)")
		}
	}

	var limited recallOut
	// JSON numbers arrive as float64 over the wire — mirror that here.
	json.Unmarshal([]byte(must("nt_recall", map[string]any{"limit": float64(1)})), &limited)
	if len(limited.Notes) != 1 {
		t.Fatalf("limit=1 should return 1 note, got %d", len(limited.Notes))
	}

	// Completed tasks are omitted from recall by default (active context only),
	// but include_done brings them back.
	var added taskOut
	json.Unmarshal([]byte(must("nt_add", map[string]any{"text": "ship it"})), &added)
	must("nt_done", map[string]any{"id": added.ID})
	var afterDone recallOut
	json.Unmarshal([]byte(must("nt_recall", map[string]any{})), &afterDone)
	for _, tk := range afterDone.Tasks {
		if tk.Status == "done" {
			t.Fatalf("default recall must omit completed tasks, got %+v", tk)
		}
	}
	var withDone recallOut
	json.Unmarshal([]byte(must("nt_recall", map[string]any{"include_done": true})), &withDone)
	if len(withDone.Tasks) <= len(afterDone.Tasks) {
		t.Fatalf("include_done should add the completed task back (%d vs %d)", len(withDone.Tasks), len(afterDone.Tasks))
	}
}
