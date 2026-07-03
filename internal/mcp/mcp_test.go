package mcp

import (
	"encoding/json"
	"fmt"
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
	return &server{eng: e, version: "test", cache: note.NewCache()}
}

// statusOut mirrors the nt_status result fields the tests assert on.
type statusOut struct {
	OpenByUrgency []taskOut `json:"openByUrgency"`
	RecentlyDone  []taskOut `json:"recentlyDone"`
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

	// nt_status surfaces it among the open-by-urgency tasks.
	out, _ = s.dispatch("nt_status", map[string]any{})
	var st statusOut
	json.Unmarshal([]byte(out), &st)
	if len(st.OpenByUrgency) != 1 || st.OpenByUrgency[0].ID != added.ID {
		t.Fatalf("nt_status should return the new task, got %+v", st.OpenByUrgency)
	}

	// nt_update status:done by stable id completes it; the task leaves the open
	// list and shows under recentlyDone.
	if _, err := s.dispatch("nt_update", map[string]any{"id": added.ID, "status": "done"}); err != nil {
		t.Fatal(err)
	}
	out, _ = s.dispatch("nt_status", map[string]any{})
	st = statusOut{}
	json.Unmarshal([]byte(out), &st)
	if len(st.OpenByUrgency) != 0 {
		t.Errorf("open list should be empty after done, got %d", len(st.OpenByUrgency))
	}
	if len(st.RecentlyDone) != 1 {
		t.Errorf("recentlyDone should list the completed task, got %d", len(st.RecentlyDone))
	}

	// Positional handles are refused (agents must use stable ids).
	if _, err := s.dispatch("nt_update", map[string]any{"id": "task:1", "status": "done"}); err == nil {
		t.Error("nt_update must reject a positional handle")
	}

	// nt_note captures a note; it shows up in nt_index as a stub (no body), and
	// nt_get round-trips the full body by id.
	nout, err := s.dispatch("nt_note", map[string]any{"title": "JWT", "body": "root cause auth.go:42", "description": "why tokens expire"})
	if err != nil {
		t.Fatal(err)
	}
	var savedNote noteOut
	json.Unmarshal([]byte(nout), &savedNote)

	out, _ = s.dispatch("nt_index", map[string]any{})
	var idx struct {
		Notes []noteStub `json:"notes"`
	}
	json.Unmarshal([]byte(out), &idx)
	if len(idx.Notes) != 1 || idx.Notes[0].Title != "JWT" || idx.Notes[0].Description != "why tokens expire" {
		t.Fatalf("nt_index should list the note stub with its description, got %+v", idx.Notes)
	}
	if strings.Contains(out, "auth.go:42") {
		t.Errorf("nt_index must NOT include note bodies, got %s", out)
	}

	gout, err := s.dispatch("nt_get", map[string]any{"handle": savedNote.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gout, "auth.go:42") {
		t.Errorf("nt_get should return the full body, got %s", gout)
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
	// The full text is preserved in the linked detail note. It lives under
	// __tasks__/ (excluded from the KB catalog/search), so fetch it by handle.
	rout, err := s.dispatch("nt_get", map[string]any{"handle": res.Note})
	if err != nil {
		t.Fatalf("nt_get %q: %v", res.Note, err)
	}
	if !strings.Contains(rout, "connection pool exhaustion") {
		t.Errorf("the note should hold the full original text: %s", rout)
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

	out, _ := s.dispatch("nt_status", map[string]any{"project": "auth"})
	var pre statusOut
	json.Unmarshal([]byte(out), &pre)
	if len(pre.OpenByUrgency) == 0 {
		t.Fatal("seeded +auth tasks should be open")
	}
	s.dispatch("nt_update", map[string]any{"id": pre.OpenByUrgency[0].ID, "status": "doing"})

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

	// nt_links: forward (note) + backlink (task). The task backlink is rendered as
	// a clean reference — short id + display text — with the [[wiki-link]] markup
	// stripped, not the raw todo.txt line.
	if out := mustDispatch("nt_links", map[string]any{"handle": "auth-design"}); !strings.Contains(out, "Token Rotation") || !strings.Contains(out, "implement @auth") {
		t.Fatalf("nt_links missing forward/clean backlink: %s", out)
	}
	if out := mustDispatch("nt_links", map[string]any{"handle": "auth-design"}); strings.Contains(out, "implement [[auth-design]]") {
		t.Errorf("task backlink should not expose raw [[link]] markup: %s", out)
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
	if out := must("nt_index", map[string]any{}); strings.Contains(out, "Stale Note") {
		t.Error("nt_index must not list an archived note")
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
	if !strings.Contains(out, `"views":[]`) {
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
	if !strings.Contains(out, `"name":"backend"`) || !strings.Contains(out, "--tag backend") {
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
		return
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

// TestMCPGetByIDAndCacheFreshness: nt_get resolves by id via the cache fast path,
// and the parse cache stays fresh — a mutation through a write handler (nt_tag) is
// visible to the next read (the cache re-stats every call).
func TestMCPGetByIDAndCacheFreshness(t *testing.T) {
	s := newServer(t)
	must := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	var saved noteOut
	json.Unmarshal([]byte(must("nt_note", map[string]any{"title": "Cache note", "body": "hello", "tags": []any{"a"}})), &saved)
	if saved.ID == "" {
		t.Fatal("note should have an id")
	}

	// get-by-id fast path returns the right note.
	got := must("nt_get", map[string]any{"handle": saved.ID})
	if !strings.Contains(got, "hello") || !strings.Contains(got, saved.ID) {
		t.Fatalf("nt_get by id should return the note: %s", got)
	}

	// A write handler mutates the note; the next read must see it (no stale cache).
	must("nt_tag", map[string]any{"handle": saved.ID, "add": []any{"fresh"}})
	if !strings.Contains(must("nt_get", map[string]any{"handle": saved.ID}), "fresh") {
		t.Error("cache served stale data: the new tag should be visible after nt_tag")
	}
	if !strings.Contains(must("nt_index", map[string]any{}), "fresh") {
		t.Error("nt_index should reflect the retag (cache re-stat)")
	}
}

// TestMCPIndexAndSearch: nt_index returns stubs (no bodies) + active tasks; done
// tasks are excluded; nt_search returns ranked stubs, caps at limit with
// truncated=true, and full=true inlines bodies.
func TestMCPIndexAndSearch(t *testing.T) {
	s := newServer(t)
	must := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	must("nt_note", map[string]any{"title": "Decision A", "folder": "decisions", "body": "long rationale here\nsecond secret line", "description": "why we chose A"})
	must("nt_note", map[string]any{"title": "Decision B", "folder": "decisions", "body": "more reasoning here"})

	type indexOut struct {
		Notes []noteStub `json:"notes"`
		Tasks []taskOut  `json:"tasks"`
	}
	var idx indexOut
	json.Unmarshal([]byte(must("nt_index", map[string]any{})), &idx)
	if len(idx.Notes) != 2 {
		t.Fatalf("index should list 2 note stubs, got %d", len(idx.Notes))
	}
	for _, n := range idx.Notes {
		if n.Title == "" {
			t.Fatal("index stub must carry a title")
		}
		if n.Description == "" {
			t.Fatalf("index stub should carry a description (explicit or body fallback), got %+v", n)
		}
	}
	// Stubs carry no body field at all.
	if strings.Contains(must("nt_index", map[string]any{}), "second secret line") {
		t.Error("nt_index must not include note bodies")
	}

	// Done tasks are excluded from the active list.
	var added taskOut
	json.Unmarshal([]byte(must("nt_add", map[string]any{"text": "ship it"})), &added)
	must("nt_update", map[string]any{"id": added.ID, "status": "done"})
	var afterDone indexOut
	json.Unmarshal([]byte(must("nt_index", map[string]any{})), &afterDone)
	for _, tk := range afterDone.Tasks {
		if tk.Status == "done" {
			t.Fatalf("nt_index must omit completed tasks, got %+v", tk)
		}
	}

	// search returns stubs by default (a one-line snippet, not the whole body);
	// full=true inlines the complete body.
	sres := must("nt_search", map[string]any{"query": "rationale"})
	if strings.Contains(sres, "second secret line") {
		t.Errorf("nt_search default must return stubs, not full bodies: %s", sres)
	}
	if !strings.Contains(must("nt_search", map[string]any{"query": "rationale", "full": true}), "second secret line") {
		t.Error("nt_search full=true should inline the body")
	}

	// limit caps results and flags truncation. Distinct titles so the dedup guard
	// doesn't refuse them.
	for i := 0; i < 5; i++ {
		must("nt_note", map[string]any{"title": fmt.Sprintf("Ref topic %d", i), "folder": "ref", "tags": []any{"bulk"}, "body": "x", "force": true})
	}
	var capped struct {
		Notes     []noteStub `json:"notes"`
		Truncated bool       `json:"truncated"`
	}
	json.Unmarshal([]byte(must("nt_search", map[string]any{"tag": "bulk", "limit": float64(2)})), &capped)
	if len(capped.Notes) != 2 || !capped.Truncated {
		t.Fatalf("limit=2 should return 2 stubs and truncated=true, got %d truncated=%v", len(capped.Notes), capped.Truncated)
	}
}

// helper: collect active task texts from nt_index's {tasks:[...]} payload.
func indexTaskTexts(t *testing.T, s *server, args map[string]any) []string {
	t.Helper()
	out, err := s.dispatch("nt_index", args)
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Tasks []taskOut `json:"tasks"`
		Notes []noteOut `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 0, len(rec.Tasks))
	for _, t := range rec.Tasks {
		texts = append(texts, t.Text)
	}
	return texts
}

func hasText(texts []string, want string) bool {
	for _, s := range texts {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// TestMCPWorkstreamIsolation: parallel agents sharing one store get isolated
// tasks but shared notes, with unstamped tasks visible to all and "*" widening.
func TestMCPWorkstreamIsolation(t *testing.T) {
	s := newServer(t)

	// A shared/legacy task with no workstream (as the CLI/TUI/web create).
	t.Setenv("NT_WORKSTREAM", "")
	if _, err := s.dispatch("nt_add", map[string]any{"text": "human backlog item"}); err != nil {
		t.Fatal(err)
	}

	// Agent on workstream feat-a captures a task and a note.
	t.Setenv("NT_WORKSTREAM", "feat-a")
	if _, err := s.dispatch("nt_add", map[string]any{"text": "wire feature A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatch("nt_note", map[string]any{"title": "A finding", "body": "shared knowledge"}); err != nil {
		t.Fatal(err)
	}

	// Agent on workstream feat-b captures its own task.
	t.Setenv("NT_WORKSTREAM", "feat-b")
	if _, err := s.dispatch("nt_add", map[string]any{"text": "wire feature B"}); err != nil {
		t.Fatal(err)
	}

	// recall as feat-a: own task + the shared/legacy task, NOT feat-b's task.
	t.Setenv("NT_WORKSTREAM", "feat-a")
	got := indexTaskTexts(t, s, map[string]any{})
	if !hasText(got, "wire feature A") || !hasText(got, "human backlog item") {
		t.Errorf("feat-a should see its own + the shared task, got %v", got)
	}
	if hasText(got, "wire feature B") {
		t.Errorf("feat-a must not see feat-b's task, got %v", got)
	}

	// Notes are shared: feat-b sees the note feat-a wrote (not workstream-scoped).
	t.Setenv("NT_WORKSTREAM", "feat-b")
	out, _ := s.dispatch("nt_index", map[string]any{})
	if !strings.Contains(out, "A finding") {
		t.Errorf("notes must be shared across workstreams, got %s", out)
	}
	gotB := indexTaskTexts(t, s, map[string]any{})
	if !hasText(gotB, "wire feature B") || hasText(gotB, "wire feature A") {
		t.Errorf("feat-b should see only its own + shared tasks, got %v", gotB)
	}

	// "*" widens a read to every workstream's tasks.
	all := indexTaskTexts(t, s, map[string]any{"workstream": "*"})
	if !hasText(all, "wire feature A") || !hasText(all, "wire feature B") {
		t.Errorf(`workstream:"*" should see all tasks, got %v`, all)
	}

	// The call arg overrides the env identity.
	override := indexTaskTexts(t, s, map[string]any{"workstream": "feat-a"})
	if !hasText(override, "wire feature A") || hasText(override, "wire feature B") {
		t.Errorf("workstream arg should override env, got %v", override)
	}
}

// TestMCPNoWorkstreamUnchanged: with no identity set, behavior is as before —
// every task is visible (no scoping).
func TestMCPNoWorkstreamUnchanged(t *testing.T) {
	s := newServer(t)
	t.Setenv("NT_WORKSTREAM", "")
	for _, txt := range []string{"one", "two", "three"} {
		if _, err := s.dispatch("nt_add", map[string]any{"text": txt}); err != nil {
			t.Fatal(err)
		}
	}
	if got := indexTaskTexts(t, s, map[string]any{}); len(got) != 3 {
		t.Errorf("no workstream should scope nothing, got %v", got)
	}
}

// TestMCPWorkstreamScopesAllReads: ready/status/log scope to the workstream too,
// not just recall (each got its own filter call site).
func TestMCPWorkstreamScopesAllReads(t *testing.T) {
	s := newServer(t)

	t.Setenv("NT_WORKSTREAM", "feat-a")
	outA, _ := s.dispatch("nt_add", map[string]any{"text": "ready in A"})
	var addedA taskOut
	json.Unmarshal([]byte(outA), &addedA)
	if addedA.Workstream != "feat-a" {
		t.Fatalf("nt_add should expose workstream in taskOut, got %+v", addedA)
	}
	// A completed task in A, for recentlyDone scoping.
	doneOut, _ := s.dispatch("nt_add", map[string]any{"text": "done in A"})
	var doneA taskOut
	json.Unmarshal([]byte(doneOut), &doneA)
	s.dispatch("nt_update", map[string]any{"id": doneA.ID, "status": "done"})

	t.Setenv("NT_WORKSTREAM", "feat-b")
	s.dispatch("nt_add", map[string]any{"text": "ready in B"})

	// nt_status as feat-b: only B's open task; A's completed task is invisible.
	t.Setenv("NT_WORKSTREAM", "feat-b")
	out, _ := s.dispatch("nt_status", map[string]any{})
	var stB statusOut
	json.Unmarshal([]byte(out), &stB)
	if len(stB.OpenByUrgency) != 1 || stB.OpenByUrgency[0].Text != "ready in B" {
		t.Errorf("nt_status should scope to feat-b, got %+v", stB.OpenByUrgency)
	}
	if strings.Contains(out, "done in A") {
		t.Errorf("nt_status must not show feat-a's completed task to feat-b, got %s", out)
	}

	// nt_status as feat-a: A's open + completed tasks present, B's absent.
	t.Setenv("NT_WORKSTREAM", "feat-a")
	out, _ = s.dispatch("nt_status", map[string]any{})
	if !strings.Contains(out, "ready in A") || strings.Contains(out, "ready in B") {
		t.Errorf("nt_status should scope to feat-a, got %s", out)
	}
	if !strings.Contains(out, "done in A") {
		t.Errorf("nt_status recentlyDone should show feat-a's completed task, got %s", out)
	}
}

// TestMCPAddStarIsShared: nt_add with workstream "*" (or an inline ws: in text)
// must NOT stamp a literal — the task lands in the shared backlog.
func TestMCPAddStarIsShared(t *testing.T) {
	s := newServer(t)
	out, _ := s.dispatch("nt_add", map[string]any{"text": "spoof ws:sneaky", "workstream": "*"})
	var added taskOut
	json.Unmarshal([]byte(out), &added)
	if added.Workstream != "" {
		t.Errorf(`"*"/inline ws must not stamp a workstream, got %q`, added.Workstream)
	}
	// Visible to a scoped agent (it's shared).
	t.Setenv("NT_WORKSTREAM", "feat-a")
	if got := indexTaskTexts(t, s, map[string]any{}); !hasText(got, "spoof") {
		t.Errorf("a shared task should be visible to a scoped agent, got %v", got)
	}
}

// TestMCPUpdateClaimsWorkstream: nt_update can claim a shared task into a
// workstream and release it back with "*".
func TestMCPUpdateClaimsWorkstream(t *testing.T) {
	s := newServer(t)
	t.Setenv("NT_WORKSTREAM", "") // create it shared
	out, _ := s.dispatch("nt_add", map[string]any{"text": "shared chore"})
	var task taskOut
	json.Unmarshal([]byte(out), &task)

	// Claim into feat-a.
	if _, err := s.dispatch("nt_update", map[string]any{"id": task.ID, "workstream": "feat-a"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NT_WORKSTREAM", "feat-b")
	if got := indexTaskTexts(t, s, map[string]any{}); hasText(got, "shared chore") {
		t.Errorf("after claim into feat-a, feat-b must not see it, got %v", got)
	}
	t.Setenv("NT_WORKSTREAM", "feat-a")
	if got := indexTaskTexts(t, s, map[string]any{}); !hasText(got, "shared chore") {
		t.Errorf("feat-a should see the task it claimed, got %v", got)
	}

	// Release back to shared with "*".
	if _, err := s.dispatch("nt_update", map[string]any{"id": task.ID, "workstream": "*"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NT_WORKSTREAM", "feat-b")
	if got := indexTaskTexts(t, s, map[string]any{}); !hasText(got, "shared chore") {
		t.Errorf("after release, feat-b should see it again, got %v", got)
	}
}

// TestMCPDedupSupersedeAndDanglingLinks: nt_note flags a near-duplicate SOFTLY
// (creates the note, returns `similar`) so parallel agents never lose a capture;
// nt_archive(superseded_by) retires a note from the index; a note with an unresolved [[link]]
// reports danglingLinks.
func TestMCPDedupSupersedeAndDanglingLinks(t *testing.T) {
	s := newServer(t)
	must := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	var first noteOut
	json.Unmarshal([]byte(must("nt_note", map[string]any{"title": "Refresh token rotation", "tags": []any{"auth"}, "body": "single-use"})), &first)

	// Near-duplicate is CREATED (no data loss) and flags the original in `similar`.
	dupOut := must("nt_note", map[string]any{"title": "Refresh token rotation strategy", "tags": []any{"auth"}})
	if !strings.Contains(dupOut, "similar") || !strings.Contains(dupOut, first.ID) {
		t.Errorf("near-duplicate should be created with a `similar` hint: %s", dupOut)
	}
	var forked noteOut
	json.Unmarshal([]byte(dupOut), &forked)
	if forked.ID == "" || forked.ID == first.ID {
		t.Errorf("near-duplicate should be a distinct new note, got %q", forked.ID)
	}

	// nt_archive with superseded_by retires the fork; nt_index no longer lists it.
	must("nt_archive", map[string]any{"handle": forked.ID, "superseded_by": first.ID})
	if strings.Contains(must("nt_index", map[string]any{}), forked.ID) {
		t.Error("superseded note should be gone from nt_index")
	}

	// A note with an unresolved [[link]] reports danglingLinks.
	out := must("nt_note", map[string]any{"title": "Links somewhere", "tags": []any{"x"}, "body": "see [[ghost]]"})
	if !strings.Contains(out, "danglingLinks") || !strings.Contains(out, "ghost") {
		t.Errorf("expected danglingLinks for an unresolved [[ghost]]: %s", out)
	}
}

// TestMCPTaskOverlapWarning: nt_add is advisory (non-blocking) about a near-
// duplicate task on the same project/tag, and about a decision note already on the
// topic — the task-layer analog of the note dedup guard.
func TestMCPTaskOverlapWarning(t *testing.T) {
	s := newServer(t)
	must := func(name string, a map[string]any) string {
		t.Helper()
		out, err := s.dispatch(name, a)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	// First task — no similar yet.
	first := must("nt_add", map[string]any{"text": "normalize watermark timestamps to UTC", "project": "pipeline"})
	if strings.Contains(first, "similar") {
		t.Errorf("first task should have no similar: %s", first)
	}
	// A near-duplicate task on the same project → flagged as similar (non-blocking).
	dup := must("nt_add", map[string]any{"text": "normalize timestamps UTC for watermark", "project": "pipeline"})
	if !strings.Contains(dup, "similar") || !strings.Contains(dup, "\"kind\":\"task\"") {
		t.Errorf("near-duplicate task should be flagged as similar: %s", dup)
	}
	// An unrelated task is not flagged.
	other := must("nt_add", map[string]any{"text": "write the deploy script", "project": "pipeline"})
	if strings.Contains(other, "similar") {
		t.Errorf("unrelated task should not be flagged: %s", other)
	}
	// A decision note on the topic → a task on it is flagged to link, not duplicate.
	must("nt_note", map[string]any{"title": "Config precedence order", "tags": []any{"clitool"}, "body": "flags > env > file"})
	tsk := must("nt_add", map[string]any{"text": "Config precedence order test", "tags": []any{"clitool"}})
	if !strings.Contains(tsk, "\"kind\":\"note\"") {
		t.Errorf("a task restating a decision note should surface it: %s", tsk)
	}
}

// TestMCPIndexCompactFormat: format:"compact" returns terse one-line text, not
// JSON — the cheap session-start load — and still lists notes and tasks.
func TestMCPIndexCompactFormat(t *testing.T) {
	s := newServer(t)
	s.dispatch("nt_note", map[string]any{"title": "Auth design", "description": "24h JWTs", "folder": "ref", "tags": []any{"auth"}})
	s.dispatch("nt_add", map[string]any{"text": "refactor middleware", "tags": []any{"backend"}})

	compact, err := s.dispatch("nt_index", map[string]any{"format": "compact"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(compact), "{") {
		t.Errorf("compact format must not be JSON:\n%s", compact)
	}
	for _, want := range []string{"NOTES", "Auth design", "24h JWTs", "TASKS", "refactor middleware"} {
		if !strings.Contains(compact, want) {
			t.Errorf("compact index missing %q:\n%s", want, compact)
		}
	}
	// Compact is materially smaller than JSON for the same catalog.
	full, _ := s.dispatch("nt_index", map[string]any{})
	if len(compact) >= len(full) {
		t.Errorf("compact (%d) should be smaller than JSON (%d)", len(compact), len(full))
	}
}

// TestMCPScopeArgArrayCoercion: an LLM passing tag/folder as a single-element
// array must still filter — not silently no-op into a full-store dump.
func TestMCPScopeArgArrayCoercion(t *testing.T) {
	s := newServer(t)
	s.dispatch("nt_note", map[string]any{"title": "Kept", "tags": []any{"auth"}, "body": "x"})
	s.dispatch("nt_note", map[string]any{"title": "Other", "tags": []any{"misc"}, "body": "y"})

	out, err := s.dispatch("nt_index", map[string]any{"tag": []any{"auth"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Kept") || strings.Contains(out, "Other") {
		t.Errorf("array-form tag arg should filter to the tagged note only:\n%s", out)
	}
}

// TestMCPUnknownToolSuggestions: an unknown tool name gets recovery guidance —
// hand-written pointers for the documented-but-never-shipped names, and a
// nearest-match "did you mean" for typos of real tools.
func TestMCPUnknownToolSuggestions(t *testing.T) {
	s := newServer(t)
	cases := []struct {
		name string
		want string
	}{
		{"nt_ready", "use nt_status (in-progress + ready tasks) or nt_index"},
		{"nt_log", "use nt_status (includes recent completions) or nt_index (recentlyDone)"},
		{"nt_done", `use nt_update with status:"done"`},
		{"nt_delete", "use nt_rm"},
		{"nt_serch", "did you mean nt_search?"},
		{"nt_stauts", "did you mean nt_status?"},
	}
	for _, c := range cases {
		_, err := s.dispatch(c.name, map[string]any{})
		if err == nil {
			t.Fatalf("%s should be unknown", c.name)
		}
		if !strings.Contains(err.Error(), "unknown tool") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s error should suggest %q, got: %v", c.name, c.want, err)
		}
	}
	// A name that isn't nt_-prefixed gets the plain error, no wild guess.
	if _, err := s.dispatch("bogus", map[string]any{}); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("non-nt names should not get a suggestion, got: %v", err)
	}
	// An nt_ name nowhere near any real tool gets no misleading suggestion.
	if _, err := s.dispatch("nt_zzzzzzzzzzzzzzzz", map[string]any{}); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("a far-off name should not get a suggestion, got: %v", err)
	}
}

// TestMCPUpdateEchoesChanges: nt_update confirms what it applied via `changed`,
// and warns when the target is already done (where the flat task shape alone
// would silently omit e.g. priority).
func TestMCPUpdateEchoesChanges(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_add", map[string]any{"text": "tune retries"})
	if err != nil {
		t.Fatal(err)
	}
	var added taskOut
	json.Unmarshal([]byte(out), &added)

	type updateOut struct {
		taskOut
		Changed map[string]string `json:"changed"`
		Note    string            `json:"note"`
	}

	// An open-task update echoes the applied fields, no done-note.
	out, err = s.dispatch("nt_update", map[string]any{"id": added.ID, "priority": "high", "due": "2030-01-02"})
	if err != nil {
		t.Fatal(err)
	}
	var up updateOut
	json.Unmarshal([]byte(out), &up)
	if up.Changed["priority"] != "A" || up.Changed["due"] != "2030-01-02" {
		t.Errorf("changed should echo the applied fields, got %v", up.Changed)
	}
	if up.Note != "" {
		t.Errorf("an open-task update should carry no done-note, got %q", up.Note)
	}

	// Updating a DONE task still applies + echoes, but warns it's already done.
	if _, err := s.dispatch("nt_update", map[string]any{"id": added.ID, "status": "done"}); err != nil {
		t.Fatal(err)
	}
	out, err = s.dispatch("nt_update", map[string]any{"id": added.ID, "priority": "low"})
	if err != nil {
		t.Fatal(err)
	}
	up = updateOut{}
	json.Unmarshal([]byte(out), &up)
	if up.Changed["priority"] != "C" {
		t.Errorf("done-task update should still echo changed, got %v", up.Changed)
	}
	if !strings.Contains(up.Note, "already done") || !strings.Contains(up.Note, `status:"open"`) {
		t.Errorf("done-task update should note the reopen path, got %q", up.Note)
	}

	// Reopening suppresses the note (the update DOES reopen it).
	out, _ = s.dispatch("nt_update", map[string]any{"id": added.ID, "status": "open"})
	up = updateOut{}
	json.Unmarshal([]byte(out), &up)
	if up.Note != "" {
		t.Errorf("a reopening update should carry no note, got %q", up.Note)
	}
	if up.Changed["status"] != "open" {
		t.Errorf("reopen should echo status change, got %v", up.Changed)
	}
}

// TestMCPProtocolVersionEcho: initialize echoes the client's requested MCP
// revision when it's one we know (tools-only surface is compatible across
// them), and falls back to the baseline otherwise.
func TestMCPProtocolVersionEcho(t *testing.T) {
	s := newServer(t)
	initVersion := func(params string) string {
		t.Helper()
		req := request{ID: json.RawMessage("1"), Method: "initialize"}
		if params != "" {
			req.Params = json.RawMessage(params)
		}
		resp, reply := s.handle(req)
		if !reply || resp.Error != nil {
			t.Fatalf("initialize failed: %+v", resp)
		}
		return resp.Result.(map[string]any)["protocolVersion"].(string)
	}
	for _, v := range []string{"2024-11-05", "2025-03-26", "2025-06-18"} {
		if got := initVersion(`{"protocolVersion":"` + v + `"}`); got != v {
			t.Errorf("known revision %s should be echoed, got %s", v, got)
		}
	}
	if got := initVersion(`{"protocolVersion":"1999-01-01"}`); got != "2024-11-05" {
		t.Errorf("unknown revision should fall back to 2024-11-05, got %s", got)
	}
	if got := initVersion(""); got != "2024-11-05" {
		t.Errorf("missing params should fall back to 2024-11-05, got %s", got)
	}
}

// TestMCPRm: nt_rm removes a mistaken task via the journaled engine (so `nt
// undo` can restore it), refuses positional handles, and errors on unknown ids.
func TestMCPRm(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_add", map[string]any{"text": "oops, wrong task"})
	if err != nil {
		t.Fatal(err)
	}
	var added taskOut
	json.Unmarshal([]byte(out), &added)

	rout, err := s.dispatch("nt_rm", map[string]any{"id": added.ID})
	if err != nil {
		t.Fatal(err)
	}
	var rm struct {
		Removed bool   `json:"removed"`
		ID      string `json:"id"`
		Text    string `json:"text"`
		Note    string `json:"note"`
	}
	json.Unmarshal([]byte(rout), &rm)
	if !rm.Removed || rm.ID != added.ID || !strings.Contains(rm.Text, "oops") {
		t.Errorf("nt_rm should echo the removed task, got %s", rout)
	}
	if !strings.Contains(rm.Note, "nt undo") {
		t.Errorf("nt_rm should point at `nt undo`, got %q", rm.Note)
	}

	// Gone from nt_index.
	if got := indexTaskTexts(t, s, map[string]any{}); hasText(got, "oops") {
		t.Errorf("removed task must be gone from nt_index, got %v", got)
	}

	// Positional handles are refused, with a pointer to where ids come from.
	if _, err := s.dispatch("nt_rm", map[string]any{"id": "task:1"}); err == nil || !strings.Contains(err.Error(), "nt_status, nt_index, or nt_search") {
		t.Errorf("nt_rm must refuse positional handles and say where ids come from, got: %v", err)
	}
	// Unknown ids error.
	if _, err := s.dispatch("nt_rm", map[string]any{"id": "01ZZZZZZZZZZZZZZZZZZZZZZZZ"}); err == nil {
		t.Error("nt_rm should error on an unknown id")
	}
	// id is required.
	if _, err := s.dispatch("nt_rm", map[string]any{}); err == nil {
		t.Error("nt_rm should require an id")
	}
}

// TestMCPToolsListSchemas: the advertised catalog includes nt_rm, and nt_index's
// format is a closed enum ["json","compact"] in the tools/list JSON.
func TestMCPToolsListSchemas(t *testing.T) {
	s := newServer(t)
	resp, _ := s.handle(request{ID: json.RawMessage("1"), Method: "tools/list"})
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Type string   `json:"type"`
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, td := range listed.Tools {
		names[td.Name] = true
		if td.Name == "nt_index" {
			f, ok := td.InputSchema.Properties["format"]
			if !ok {
				t.Fatal("nt_index should advertise a format property")
			}
			if len(f.Enum) != 2 || f.Enum[0] != "json" || f.Enum[1] != "compact" {
				t.Errorf(`nt_index format should be enum ["json","compact"], got %v`, f.Enum)
			}
		}
	}
	if !names["nt_rm"] {
		t.Error("tools/list should advertise nt_rm")
	}
}

// TestMCPStarReadCarriesWorkstream: a "*" (all-workstreams) read tags each task
// with its workstream, so the agent can tell whose is whose.
func TestMCPStarReadCarriesWorkstream(t *testing.T) {
	s := newServer(t)
	t.Setenv("NT_WORKSTREAM", "feat-a")
	s.dispatch("nt_add", map[string]any{"text": "task in A"})
	t.Setenv("NT_WORKSTREAM", "feat-b")
	s.dispatch("nt_add", map[string]any{"text": "task in B"})

	out, err := s.dispatch("nt_index", map[string]any{"workstream": "*"})
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Tasks []taskOut `json:"tasks"`
	}
	json.Unmarshal([]byte(out), &idx)
	byText := map[string]string{}
	for _, tk := range idx.Tasks {
		byText[tk.Text] = tk.Workstream
	}
	if byText["task in A"] != "feat-a" || byText["task in B"] != "feat-b" {
		t.Errorf(`"*" read should carry each task's workstream, got %v`, byText)
	}

	// nt_status "*" carries it too.
	sout, _ := s.dispatch("nt_status", map[string]any{"workstream": "*"})
	var st statusOut
	json.Unmarshal([]byte(sout), &st)
	seen := map[string]string{}
	for _, tk := range st.OpenByUrgency {
		seen[tk.Text] = tk.Workstream
	}
	if seen["task in A"] != "feat-a" || seen["task in B"] != "feat-b" {
		t.Errorf(`nt_status "*" should carry each task's workstream, got %v`, seen)
	}
}

// TestMCPSearchIsStoreWide: nt_search is the "does this exist anywhere?" verb —
// its task results are NEVER workstream-scoped (unlike nt_index/nt_status), per
// the documented contract. Scoping made a match in another agent's workstream
// come back empty, which readers took as "no such task" and duplicated the work.
func TestMCPSearchIsStoreWide(t *testing.T) {
	s := newServer(t)
	t.Setenv("NT_WORKSTREAM", "webhookd")
	if _, err := s.dispatch("nt_add", map[string]any{"text": "verify HMAC signatures on inbound webhooks"}); err != nil {
		t.Fatal(err)
	}

	// A different workstream still finds the task by text.
	t.Setenv("NT_WORKSTREAM", "mcpbuild")
	out, err := s.dispatch("nt_search", map[string]any{"query": "HMAC", "type": "task"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Tasks []taskOut `json:"tasks"`
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Tasks) != 1 || res.Tasks[0].Workstream != "webhookd" {
		t.Fatalf("search must find another workstream's task (store-wide), got %s", out)
	}
}

// TestMCPSearchEmptyResultsEncodeAsArrays: no matches must come back as
// "tasks":[] / "notes":[], never null — a null reads as breakage to an agent.
func TestMCPSearchEmptyResultsEncodeAsArrays(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_search", map[string]any{"query": "nothing matches this"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"tasks":[]`) || !strings.Contains(out, `"notes":[]`) {
		t.Fatalf("empty search results must encode as [] not null: %s", out)
	}
	// type:"task" leaves the notes side untouched — it still must be [], not null.
	out, err = s.dispatch("nt_search", map[string]any{"query": "nothing matches this", "type": "task"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "null") {
		t.Fatalf("type-scoped empty search must not emit null arrays: %s", out)
	}
}

// TestMCPPriorityMediumAcceptedAndEchoed: the advertised enum includes "medium"
// (dateparse always accepted it — the schema just lied), and both nt_add and
// nt_update echo the canonical stored priority ("B") so the agent gets positive
// confirmation of what the store now holds.
func TestMCPPriorityMediumAcceptedAndEchoed(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_add", map[string]any{"text": "medium priority task", "priority": "medium"})
	if err != nil {
		t.Fatal(err)
	}
	var added taskOut
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if added.Priority != "B" {
		t.Fatalf(`nt_add priority:"medium" should echo canonical "B", got %+v`, added)
	}

	uout, err := s.dispatch("nt_update", map[string]any{"id": added.ID, "priority": "medium"})
	if err != nil {
		t.Fatal(err)
	}
	var upd struct {
		Priority string            `json:"priority"`
		Changed  map[string]string `json:"changed"`
	}
	json.Unmarshal([]byte(uout), &upd)
	if upd.Priority != "B" || upd.Changed["priority"] != "B" {
		t.Fatalf(`nt_update priority:"medium" should echo canonical "B", got %s`, uout)
	}

	// The advertised schemas match reality: "medium" is in both enums.
	for _, name := range []string{"nt_add", "nt_update"} {
		found := false
		for _, td := range toolDefs {
			if td.Name != name {
				continue
			}
			props := td.InputSchema["properties"].(map[string]any)
			pri := props["priority"].(map[string]any)
			for _, v := range pri["enum"].([]string) {
				if v == "medium" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s priority enum should advertise \"medium\"", name)
		}
	}
}

// TestMCPNoteProjectFrontmatter: nt_note's project param stores project:
// frontmatter, and nt_recall's project boost matches it (projectMatch true)
// even when the note carries no project tag or folder.
func TestMCPNoteProjectFrontmatter(t *testing.T) {
	s := newServer(t)
	t.Setenv("NT_WORKSTREAM", "")
	if _, err := s.dispatch("nt_note", map[string]any{
		"title": "cache invalidation strategy", "body": "the cache design",
		"project": "gamma", "description": "how invalidation works",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := s.dispatch("nt_recall", map[string]any{"context": "improving the cache invalidation layer", "project": "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Results []struct {
			Title        string `json:"title"`
			ProjectMatch bool   `json:"projectMatch"`
		} `json:"results"`
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Results) == 0 || !res.Results[0].ProjectMatch {
		t.Fatalf("project: frontmatter should trigger the recall project boost: %s", out)
	}
}

// nt_note_edit is the MCP counterpart of `nt edit`: it fixes an EXISTING note
// in place instead of nt_note's create-or-supersede (new id) path — the gap
// MCP-only agents hit hardest when a note needed a small correction.
func TestMCPNoteEditModes(t *testing.T) {
	s := newServer(t)
	created, err := s.dispatch("nt_note", map[string]any{"title": "edit target", "body": "original body"})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(created), &n)

	// append
	out, err := s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "append": "more detail"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "more detail") || !strings.Contains(out, "original body") {
		t.Fatalf("append should keep the original body and add to it: %s", out)
	}

	// full body replace
	out, err = s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "body": "replaced entirely"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "replaced entirely") || strings.Contains(out, "original body") {
		t.Fatalf("body should fully replace, got %s", out)
	}

	// targeted old_string/new_string patch
	if _, err := s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "body": "the cach layer needs work"}); err != nil {
		t.Fatal(err)
	}
	out, err = s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "old_string": "cach layer", "new_string": "cache layer"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cache layer") || strings.Contains(out, "cach layer") {
		t.Fatalf("targeted patch didn't apply cleanly: %s", out)
	}

	// description alongside a body edit
	out, err = s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "append": "note", "description": "a fresh summary"})
	if err != nil {
		t.Fatal(err)
	}
	var edited struct {
		Description string `json:"description"`
	}
	json.Unmarshal([]byte(out), &edited)
	if edited.Description != "a fresh summary" {
		t.Fatalf("description should update alongside a body edit, got %q", edited.Description)
	}
}

func TestMCPNoteEditOldStringAmbiguousRefuses(t *testing.T) {
	s := newServer(t)
	created, _ := s.dispatch("nt_note", map[string]any{"title": "dup text note", "body": "foo bar\nfoo bar"})
	var n noteOut
	json.Unmarshal([]byte(created), &n)
	_, err := s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "old_string": "foo bar", "new_string": "baz"})
	if err == nil {
		t.Fatal("an old_string matching twice must be refused, not guessed at")
	}
	if !strings.Contains(err.Error(), "2 times") {
		t.Fatalf("refusal should say how many times it matched: %v", err)
	}
}

func TestMCPNoteEditRequiresPairAndOneMode(t *testing.T) {
	s := newServer(t)
	created, _ := s.dispatch("nt_note", map[string]any{"title": "pairing note", "body": "some text"})
	var n noteOut
	json.Unmarshal([]byte(created), &n)

	if _, err := s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "old_string": "some text"}); err == nil {
		t.Fatal("old_string without new_string must be rejected")
	}
	if _, err := s.dispatch("nt_note_edit", map[string]any{"handle": n.ID, "append": "x", "body": "y"}); err == nil {
		t.Fatal("mixing append and body must be rejected")
	}
	if _, err := s.dispatch("nt_note_edit", map[string]any{"handle": n.ID}); err == nil {
		t.Fatal("a call with nothing to edit must be rejected")
	}
}

// nt_note_edit accepts an "id" alias for "handle", matching nt_get's pattern —
// agents naturally reuse the id field name a stub just handed them.
func TestMCPNoteEditAcceptsIDAlias(t *testing.T) {
	s := newServer(t)
	created, _ := s.dispatch("nt_note", map[string]any{"title": "id alias note", "body": "text"})
	var n noteOut
	json.Unmarshal([]byte(created), &n)
	if _, err := s.dispatch("nt_note_edit", map[string]any{"id": n.ID, "append": "more"}); err != nil {
		t.Fatalf("id should work as a handle alias: %v", err)
	}
}
