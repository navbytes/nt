package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// kind:"memory" files under memory/ with the memory-core tag (the always-loaded
// core-memory layer) — NOT a bare "memory" tag — matching the CLI's --kind memory.
func TestMCPNoteKindMemory(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "prefers tabs", "kind": "memory"})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	if !contains(n.Tags, "memory-core") {
		t.Fatalf("kind memory should tag 'memory-core', got %v", n.Tags)
	}
	if contains(n.Tags, "memory") {
		t.Fatalf("kind memory must NOT add a bare 'memory' tag, got %v", n.Tags)
	}
	gout, err := s.dispatch("nt_get", map[string]any{"handle": n.ID})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Folder string `json:"folder"`
	}
	json.Unmarshal([]byte(gout), &got)
	if got.Folder != "memory" {
		t.Fatalf("kind memory should file under memory/, got folder %q", got.Folder)
	}
	// Invalid kinds list the full closed set.
	if _, err := s.dispatch("nt_note", map[string]any{"title": "bad", "kind": "todo"}); err == nil || !strings.Contains(err.Error(), "memory") {
		t.Fatalf("invalid kind should list lesson|decision|ref|rule|memory, got %v", err)
	}
}

// nt_add/nt_update accept blocked_by and blocks, mirroring the CLI's dependency
// semantics: loud on a bogus id, blocked visibility in nt_status/nt_index, and
// blocks:"none" clears the edge.
func TestMCPDependencyParams(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_add", map[string]any{"text": "decide schema"})
	if err != nil {
		t.Fatal(err)
	}
	var a taskOut
	json.Unmarshal([]byte(out), &a)

	out, err = s.dispatch("nt_add", map[string]any{"text": "run migration", "blocked_by": a.ID})
	if err != nil {
		t.Fatal(err)
	}
	var b taskOut
	json.Unmarshal([]byte(out), &b)

	// nt_status shows B as blocked, not open.
	out, _ = s.dispatch("nt_status", map[string]any{})
	var st struct {
		Blocked       []taskOut `json:"blocked"`
		OpenByUrgency []taskOut `json:"openByUrgency"`
	}
	json.Unmarshal([]byte(out), &st)
	if len(st.Blocked) != 1 || st.Blocked[0].ID != b.ID {
		t.Fatalf("nt_status should show the dependent task blocked, got %+v", st.Blocked)
	}

	// A already holds a blocks: edge — a second blocked_by pointing at A refuses.
	if _, err := s.dispatch("nt_add", map[string]any{"text": "another dependent", "blocked_by": a.ID}); err == nil || !strings.Contains(err.Error(), "already blocks") {
		t.Fatalf("blocked_by on a blocker with an existing edge should refuse, got %v", err)
	}

	// update blocks:"none" on the blocker clears the edge → B unblocks.
	if _, err := s.dispatch("nt_update", map[string]any{"id": a.ID, "blocks": "none"}); err != nil {
		t.Fatal(err)
	}
	out, _ = s.dispatch("nt_status", map[string]any{})
	st.Blocked = nil
	json.Unmarshal([]byte(out), &st)
	if len(st.Blocked) != 0 {
		t.Fatalf("blocks:none should unblock the dependent, got %+v", st.Blocked)
	}

	// Re-link via nt_update blocked_by, then verify the edge exists again.
	if _, err := s.dispatch("nt_update", map[string]any{"id": b.ID, "blocked_by": a.ID}); err != nil {
		t.Fatal(err)
	}
	out, _ = s.dispatch("nt_status", map[string]any{})
	st.Blocked = nil
	json.Unmarshal([]byte(out), &st)
	if len(st.Blocked) != 1 || st.Blocked[0].ID != b.ID {
		t.Fatalf("nt_update blocked_by should re-create the edge, got %+v", st.Blocked)
	}

	// Bogus ids fail LOUDLY, pointing at where real ids come from.
	if _, err := s.dispatch("nt_add", map[string]any{"text": "x", "blocked_by": "zzzzzz"}); err == nil || !strings.Contains(err.Error(), "ids come from nt_status") {
		t.Fatalf("bogus blocked_by id should error loudly, got %v", err)
	}
	if _, err := s.dispatch("nt_update", map[string]any{"id": b.ID, "blocks": "zzzzzz"}); err == nil || !strings.Contains(err.Error(), "no unique task") {
		t.Fatalf("bogus blocks id should error loudly, got %v", err)
	}

	// blocked_by:"none" is the wrong clearing move — teach the right one.
	for _, tool := range []string{"nt_add", "nt_update"} {
		args := map[string]any{"blocked_by": "none"}
		if tool == "nt_add" {
			args["text"] = "y"
		} else {
			args["id"] = b.ID
		}
		if _, err := s.dispatch(tool, args); err == nil || !strings.Contains(err.Error(), "--blocked-by takes a task id") {
			t.Fatalf("%s blocked_by:none should return the teaching error, got %v", tool, err)
		}
	}
}

// nt_index must not silently drop dependency-blocked tasks: they show in a
// separate blocked list (JSON) / BLOCKED section (compact).
func TestMCPIndexBlockedVisibility(t *testing.T) {
	s := newServer(t)
	out, _ := s.dispatch("nt_add", map[string]any{"text": "decide schema"})
	var a taskOut
	json.Unmarshal([]byte(out), &a)
	out, _ = s.dispatch("nt_add", map[string]any{"text": "run migration", "blocked_by": a.ID})
	var b taskOut
	json.Unmarshal([]byte(out), &b)

	out, err := s.dispatch("nt_index", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Tasks   []taskOut `json:"tasks"`
		Blocked []taskOut `json:"blocked"`
	}
	json.Unmarshal([]byte(out), &idx)
	if len(idx.Blocked) != 1 || idx.Blocked[0].ID != b.ID {
		t.Fatalf("nt_index should list the blocked task separately, got %+v", idx.Blocked)
	}
	for _, tk := range idx.Tasks {
		if tk.ID == b.ID {
			t.Fatal("a blocked task must not appear in the active tasks list")
		}
	}

	cout, err := s.dispatch("nt_index", map[string]any{"format": "compact"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cout, "BLOCKED (1)") || !strings.Contains(cout, "run migration") {
		t.Fatalf("compact index should have a BLOCKED section:\n%s", cout)
	}
}

// updated_since follows the CLI's past-relative grammar (14d = the last 14
// days), errors on garbage instead of silently returning the full store, and
// warns on a future-anchored value.
func TestMCPIndexUpdatedSince(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "fresh finding"}); err != nil {
		t.Fatal(err)
	}

	out, err := s.dispatch("nt_index", map[string]any{"updated_since": "14d"})
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Notes   []noteStub `json:"notes"`
		Warning string     `json:"warning"`
	}
	json.Unmarshal([]byte(out), &idx)
	if len(idx.Notes) != 1 || idx.Notes[0].Title != "fresh finding" {
		t.Fatalf("updated_since 14d should include a note created today, got %+v", idx.Notes)
	}
	if idx.Warning != "" {
		t.Fatalf("14d is past-anchored — no warning expected, got %q", idx.Warning)
	}

	// Garbage → a loud error with the accepted grammar.
	if _, err := s.dispatch("nt_index", map[string]any{"updated_since": "garbage"}); err == nil ||
		!strings.Contains(err.Error(), `invalid updated_since "garbage"`) ||
		!strings.Contains(err.Error(), "14d = last 14 days") {
		t.Fatalf("garbage updated_since should error with the grammar, got %v", err)
	}

	// A future cutoff still works but carries a warning (and matches nothing).
	future := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	out, err = s.dispatch("nt_index", map[string]any{"updated_since": future})
	if err != nil {
		t.Fatal(err)
	}
	idx = struct {
		Notes   []noteStub `json:"notes"`
		Warning string     `json:"warning"`
	}{}
	json.Unmarshal([]byte(out), &idx)
	if !strings.Contains(idx.Warning, "future date") || !strings.Contains(idx.Warning, "14d") {
		t.Fatalf("future updated_since should warn, got %q", idx.Warning)
	}
	if len(idx.Notes) != 0 {
		t.Fatalf("future cutoff should match nothing, got %+v", idx.Notes)
	}
}

// Bare lessons_only:true (no context) enumerates every lesson newest-first —
// the "what mistakes are on record?" read. No context and no lessons_only stays
// an error.
func TestMCPRecallBareLessonsOnly(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "goroutine leak on retry", "kind": "lesson"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatch("nt_note", map[string]any{"title": "flock over sqlite", "kind": "decision"}); err != nil {
		t.Fatal(err)
	}

	out, err := s.dispatch("nt_recall", map[string]any{"lessons_only": true})
	if err != nil {
		t.Fatalf("nt_recall lessons_only without context should enumerate lessons, got %v", err)
	}
	var res struct {
		Results []struct {
			Title  string `json:"title"`
			Lesson bool   `json:"lesson"`
		} `json:"results"`
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Results) != 1 || res.Results[0].Title != "goroutine leak on retry" || !res.Results[0].Lesson {
		t.Fatalf("expected exactly the lesson note, got %+v", res.Results)
	}

	// Neither context nor lessons_only → still the required-context error.
	if _, err := s.dispatch("nt_recall", map[string]any{}); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("bare nt_recall should still require context, got %v", err)
	}
}

// Retired/never-shipped tool names point at the real coverage in one turn.
func TestMCPRetiredToolHints(t *testing.T) {
	cases := map[string]string{
		"nt_gc":   "nt gc",
		"nt_undo": "nt undo",
		"nt_show": "nt_get",
	}
	s := newServer(t)
	for name, want := range cases {
		_, err := s.dispatch(name, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("dispatch(%s) should hint %q, got %v", name, want, err)
		}
	}
}

// content: is the habitual misname for body: — the alias map must nudge before
// the edit-distance fallback gives up.
func TestMCPParamAliasNudge(t *testing.T) {
	s := newServer(t)
	_, err := s.dispatch("nt_note", map[string]any{"title": "x", "content": "the finding"})
	if err == nil || !strings.Contains(err.Error(), `did you mean "body"`) {
		t.Fatalf("content should nudge toward body, got %v", err)
	}
	_, err = s.dispatch("nt_recall", map[string]any{"query": "cache layer"})
	if err == nil || !strings.Contains(err.Error(), `did you mean "context"`) {
		t.Fatalf("query on nt_recall should nudge toward context, got %v", err)
	}
}
