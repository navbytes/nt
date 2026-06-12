package web

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/view"
	"github.com/navbytes/nt/internal/web/apitypes"
)

func mustValues(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, body)
	}
	return v
}

func TestAPIStateAndNotes(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Alpha", "# Alpha\n\nbody with [[Beta]]", nil, "claude", "")
	note.Create(s.eng.S, "Beta", "b", nil, "cli", "")
	addTask(t, s, "open one")

	_, body := get(t, s, "/api/state")
	st := decode[apitypes.State](t, body)
	if st.NoteCount != 2 || st.OpenCount != 1 || st.CanEdit {
		t.Fatalf("state wrong: %+v", st)
	}

	_, body = get(t, s, "/api/notes")
	idx := decode[apitypes.NotesIndex](t, body)
	if len(idx.Index) != 2 || len(idx.Tree) == 0 {
		t.Fatalf("notes index wrong: %+v", idx)
	}

	_, body = get(t, s, "/api/notes/"+n.ID)
	nv := decode[apitypes.NoteView](t, body)
	if nv.Title != "Alpha" || !strings.Contains(nv.BodyHTML, "wikilink") || nv.ETag == "" {
		t.Fatalf("note view wrong: %+v", nv)
	}
}

func TestAPITasksReadAndWrite(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	id := addTask(t, s, "ship it")

	_, body := get(t, s, "/api/tasks")
	if g := decode[apitypes.TasksResponse](t, body).Groups; len(g) == 0 {
		t.Fatal("expected task groups")
	}

	// gate: no CSRF → 403
	if code, _ := postForm(s, "/api/tasks/"+id+"/done", "", nil); code != 403 {
		t.Errorf("done without CSRF should 403, got %d", code)
	}
	code, body := postForm(s, "/api/tasks/"+id+"/done", s.csrf, nil)
	if code != 200 {
		t.Fatalf("done: %d", code)
	}
	if !mustDoc(t, s).FindByID(id).Done {
		t.Fatal("task should be done")
	}
	if g := decode[apitypes.TasksResponse](t, body).Groups; len(g) == 0 || g[0].Status == "" {
		t.Fatalf("done response should return groups: %s", body)
	}

	code, body = postForm(s, "/api/tasks", s.csrf, mustValues("text", "new via api", "pri", "high"))
	if code != 200 {
		t.Fatalf("new: %d", code)
	}
	if !strings.Contains(body, "new via api") {
		t.Fatalf("new task not in response: %s", body)
	}
}

// TestAPITaskBoardTransitions covers the board's wire needs: priority is
// exposed (the card color cue), and dragging a *done* task back to Doing
// un-dones it (the API can't leave a task both done and in-progress).
func TestAPITaskBoardTransitions(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	code, body := postForm(s, "/api/tasks", s.csrf, mustValues("text", "fix it", "pri", "high"))
	if code != 200 {
		t.Fatalf("new: %d", code)
	}
	if !strings.Contains(body, `"priority":"A"`) {
		t.Errorf("priority should be in the tasks response (board color cue): %s", body)
	}
	var id string
	for _, g := range decode[apitypes.TasksResponse](t, body).Groups {
		for _, tk := range g.Tasks {
			if strings.Contains(tk.Text, "fix it") {
				id = tk.ID
			}
		}
	}
	if id == "" {
		t.Fatal("created task not found in response")
	}

	if code, _ := postForm(s, "/api/tasks/"+id+"/done", s.csrf, nil); code != 200 {
		t.Fatalf("done: %d", code)
	}
	if !mustDoc(t, s).FindByID(id).Done {
		t.Fatal("task should be done")
	}
	if code, _ := postForm(s, "/api/tasks/"+id+"/status", s.csrf, mustValues("status", "doing")); code != 200 {
		t.Fatalf("status doing: %d", code)
	}
	tk := mustDoc(t, s).FindByID(id)
	if tk.Done {
		t.Error("moving a done task to Doing should un-done it")
	}
	if tk.State() != "doing" {
		t.Errorf("state should be doing, got %q", tk.State())
	}
}

// TestAPIQuickAddNormalizesInlineTokens: the web quick-add box sends raw text;
// inline natural-language tokens ("due:fri", "!high") must be normalized into a
// real due date + priority rather than stored as the literal string (the bug the
// quickadd package fixes). Regression guard for the cross-surface add path.
func TestAPIQuickAddNormalizesInlineTokens(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true

	code, _ := postForm(s, "/api/tasks", s.csrf, mustValues("text", "pay rent due:fri !high @home"))
	if code != 200 {
		t.Fatalf("quick-add: %d", code)
	}
	tk := mustDoc(t, s).Tasks()[0]
	if tk.Due() == "" || tk.Due() == "fri" {
		t.Fatalf("inline due:fri should normalize to a date, got %q", tk.Due())
	}
	if _, err := timeParseISO(tk.Due()); err != nil {
		t.Fatalf("due should be an ISO date, got %q", tk.Due())
	}
	if tk.Priority != 'A' {
		t.Errorf("inline !high should lift to priority A, got %q", tk.Priority)
	}
	if tags := tk.Tags(); len(tags) != 1 || tags[0] != "home" {
		t.Errorf("@home context should survive: %v", tags)
	}
}

func timeParseISO(s string) (any, error) { return time.Parse("2006-01-02", s) }

// TestAPINoteCreate: POST /api/notes is edit+CSRF gated, creates the note, splits
// a "folder/Title" handle into a subfolder, and returns a usable handle/url.
func TestAPINoteCreate(t *testing.T) {
	s := newTestServer(t)

	// gated off when read-only
	if code, _ := postForm(s, "/api/notes", "", mustValues("title", "Nope")); code != 403 {
		t.Errorf("create should 403 when read-only, got %d", code)
	}
	s.allowEdit = true
	if code, _ := postForm(s, "/api/notes", "", mustValues("title", "Nope")); code != 403 {
		t.Errorf("create without CSRF should 403, got %d", code)
	}
	// empty title rejected
	if code, _ := postForm(s, "/api/notes", s.csrf, mustValues("title", "  ")); code != 400 {
		t.Errorf("empty title should 400, got %d", code)
	}

	code, body := postForm(s, "/api/notes", s.csrf, mustValues("title", "work/Auth Design"))
	if code != 200 {
		t.Fatalf("create: %d %s", code, body)
	}
	res := decode[apitypes.CreatedNote](t, body)
	if res.Handle == "" || res.URL == "" {
		t.Fatalf("create response missing handle/url: %+v", res)
	}
	// The note exists, filed under work/, titled without the folder prefix.
	notes, _ := note.List(s.eng.S)
	var found *note.Note
	for _, n := range notes {
		if n.Title == "Auth Design" {
			found = n
		}
	}
	if found == nil {
		t.Fatalf("created note not found among %d notes", len(notes))
	}
	if !strings.HasPrefix(found.Rel, "work/") {
		t.Errorf("note should be filed under work/, got rel %q", found.Rel)
	}
	if found.Source != "web" {
		t.Errorf("note source should be web, got %q", found.Source)
	}

	// A traversal-y folder is rejected at the boundary (path-injection guard).
	if code, _ := postForm(s, "/api/notes", s.csrf, mustValues("title", "x", "folder", "../../etc")); code != 400 {
		t.Errorf("traversal folder should 400, got %d", code)
	}
}

func TestAPINoteRawSaveGuard(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Edit Me", "v1", nil, "cli", "")

	// read-only → raw 404
	if resp, _ := get(t, s, "/api/notes/"+n.ID+"/raw"); resp.StatusCode != 404 {
		t.Errorf("raw should 404 read-only, got %d", resp.StatusCode)
	}
	s.allowEdit = true
	_, body := get(t, s, "/api/notes/"+n.ID+"/raw")
	raw := decode[apitypes.RawNote](t, body)
	if !strings.Contains(raw.Text, "v1") || raw.ETag == "" {
		t.Fatalf("raw wrong: %+v", raw)
	}

	// concurrent write → stale save 409
	if err := store.WriteAtomic(n.Path, []byte("---\nid: "+n.ID+"\n---\n\nagent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/notes/"+n.ID, strings.NewReader("clobber\n"))
	req.Header.Set("X-CSRF", s.csrf)
	req.Header.Set("If-Match", raw.ETag)
	s.routes().ServeHTTP(rec, req)
	if rec.Code != 409 {
		t.Fatalf("stale save should 409, got %d", rec.Code)
	}
}

func TestAPITagsAndOrphans(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Tagged", "body", []string{"spec"}, "cli", "")
	note.Create(s.eng.S, "A", "see [[B]]", nil, "cli", "")
	note.Create(s.eng.S, "B", "hi", nil, "cli", "")

	_, body := get(t, s, "/api/tags")
	tags := decode[apitypes.TagsResponse](t, body)
	if len(tags.Tags) != 1 || tags.Tags[0].Name != "spec" || tags.Tags[0].Count != 1 {
		t.Fatalf("tags wrong: %+v", tags)
	}

	// Orphans = notes with no inbound link. B is linked from A, so it's not an
	// orphan; Tagged (nothing links to it) is.
	_, body = get(t, s, "/api/orphans")
	orph := decode[apitypes.OrphansResponse](t, body)
	found := false
	for _, n := range orph.Notes {
		if n.Title == "Tagged" {
			found = true
		}
		if n.Title == "B" {
			t.Errorf("B has an inbound link and should not be an orphan")
		}
	}
	if !found {
		t.Fatalf("expected Tagged among orphans: %+v", orph)
	}
}

// TestAPIGraphTaskNodes: a task that wikilinks a note joins the graph as a
// "task" node with an edge to that note; tasks with no note links are omitted.
func TestAPIGraphTaskNodes(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	note.Create(s.eng.S, "Hub", "hub note", nil, "cli", "")
	addTask(t, s, "wire [[Hub]] up")
	addTask(t, s, "unconnected chore") // no note link → should NOT appear

	_, body := get(t, s, "/api/graph")
	g := decode[apitypes.GraphData](t, body)

	var notes, tasks int
	for _, n := range g.Nodes {
		switch n.Kind {
		case "note":
			notes++
		case "task":
			tasks++
			if n.URL != "/tasks" {
				t.Errorf("task node URL should be /tasks, got %q", n.URL)
			}
		}
	}
	if notes != 1 || tasks != 1 {
		t.Fatalf("want 1 note + 1 connected task node, got %d notes / %d tasks: %+v", notes, tasks, g.Nodes)
	}
	if len(g.Links) != 1 {
		t.Fatalf("want one task→note edge, got %d", len(g.Links))
	}
}

func TestAPIActivityGraphSearch(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Hub", "see [[Spoke]]", nil, "claude", "")
	note.Create(s.eng.S, "Spoke", "x", nil, "cli", "")

	_, body := get(t, s, "/api/activity")
	if act := decode[apitypes.ActivityResponse](t, body); len(act.Days) == 0 || !contains(act.Sources, "claude") {
		t.Fatalf("activity wrong: %+v", act)
	}
	_, body = get(t, s, "/api/graph")
	g := decode[graphData](t, body)
	if len(g.Nodes) != 2 || len(g.Links) != 1 {
		t.Fatalf("graph wrong: %d nodes %d links", len(g.Nodes), len(g.Links))
	}
	_, body = get(t, s, "/api/search?q=Spoke")
	if r := decode[apitypes.SearchResponse](t, body); len(r.Results) == 0 {
		t.Fatalf("search returned nothing: %s", body)
	}
}

// TestAPISearchRankingAndSnippets: title matches rank before body matches, and a
// body match carries the matching line as a snippet.
func TestAPISearchRankingAndSnippets(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Mutex Guide", "how to use locks", nil, "cli", "")
	note.Create(s.eng.S, "Race Conditions", "always guard shared state with a mutex lock", nil, "cli", "")

	_, body := get(t, s, "/api/search?q=mutex")
	res := decode[apitypes.SearchResponse](t, body).Results
	if len(res) < 2 {
		t.Fatalf("expected ≥2 results, got %d: %s", len(res), body)
	}
	if res[0].Title != "Mutex Guide" {
		t.Errorf("title match should rank first, got %q", res[0].Title)
	}
	var bodyHit *apitypes.SearchResult
	for i := range res {
		if res[i].Title == "Race Conditions" {
			bodyHit = &res[i]
		}
	}
	if bodyHit == nil || bodyHit.Snippet == "" || !strings.Contains(bodyHit.Snippet, "mutex") {
		t.Fatalf("body match should carry a snippet with the match: %+v", bodyHit)
	}
}

func TestAPISearchIncludesTasks(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Mutex Guide", "how to use locks", nil, "cli", "")
	addTask(t, s, "fix the mutex deadlock in the scheduler")
	addTask(t, s, "unrelated errand")

	_, body := get(t, s, "/api/search?q=mutex")
	res := decode[apitypes.SearchResponse](t, body).Results

	var taskHit *apitypes.SearchResult
	for i := range res {
		if res[i].Kind == "task" {
			taskHit = &res[i]
		}
	}
	if taskHit == nil {
		t.Fatalf("a matching task should appear in search results: %s", body)
	}
	if taskHit.URL != "/tasks" {
		t.Errorf("task result should link to /tasks, got %q", taskHit.URL)
	}
	if !strings.Contains(taskHit.Title, "deadlock") {
		t.Errorf("task title should be the cleaned task text, got %q", taskHit.Title)
	}
	// The non-matching task must not surface, and notes rank ahead of tasks.
	for _, r := range res {
		if r.Kind == "task" && strings.Contains(r.Title, "errand") {
			t.Errorf("non-matching task leaked into results: %+v", r)
		}
	}
	if res[0].Kind == "task" {
		t.Errorf("notes should rank before tasks; first result was a task: %+v", res[0])
	}
}

// TestAPIJournal: the journal index lists existing daily notes (journal/<date>)
// newest-first with handles, plus today's date — and ignores non-date notes.
func TestAPIJournal(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "2026-06-06", "older", nil, "cli", "journal")
	note.Create(s.eng.S, "2026-06-08", "newer", nil, "cli", "journal")
	note.Create(s.eng.S, "Not A Date", "x", nil, "cli", "journal") // must be ignored

	_, body := get(t, s, "/api/journal")
	jr := decode[apitypes.JournalResponse](t, body)
	if jr.Folder != "journal" || jr.Today == "" {
		t.Fatalf("journal meta wrong: %+v", jr)
	}
	if len(jr.Days) != 2 {
		t.Fatalf("expected 2 daily notes, got %d: %+v", len(jr.Days), jr.Days)
	}
	if jr.Days[0].Date != "2026-06-08" || jr.Days[1].Date != "2026-06-06" {
		t.Errorf("days should be newest-first: %+v", jr.Days)
	}
	if jr.Days[0].Handle == "" {
		t.Error("each day should carry a note handle")
	}
}

// TestAPISearchCap: a query matching more than maxSearchResults is capped and
// flagged truncated (E4).
func TestAPISearchCap(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < maxSearchResults+10; i++ {
		note.Create(s.eng.S, fmt.Sprintf("Widget %03d", i), "all about widgets", nil, "cli", "")
	}
	_, body := get(t, s, "/api/search?q=widget")
	r := decode[apitypes.SearchResponse](t, body)
	if len(r.Results) != maxSearchResults || !r.Truncated {
		t.Fatalf("search should cap at %d and flag truncated, got %d trunc=%v", maxSearchResults, len(r.Results), r.Truncated)
	}
}

func TestCapGraph(t *testing.T) {
	g := &graphData{}
	for i := 0; i < maxGraphNodes+50; i++ {
		g.Nodes = append(g.Nodes, graphNode{ID: fmt.Sprintf("n%d", i), Deg: i}) // higher i = higher degree
	}
	g.Links = []graphLink{
		{S: maxGraphNodes + 10, T: maxGraphNodes + 20}, // both high-degree → kept
		{S: 0, T: 1}, // both low-degree → dropped
	}
	out := capGraph(g)
	if len(out.Nodes) != maxGraphNodes || !out.Truncated {
		t.Fatalf("expected %d nodes truncated, got %d trunc=%v", maxGraphNodes, len(out.Nodes), out.Truncated)
	}
	if len(out.Links) != 1 {
		t.Errorf("only links between kept (high-degree) nodes should survive, got %d", len(out.Links))
	}
	// Survivors are the highest-degree nodes.
	for _, n := range out.Nodes {
		if n.Deg < 50 {
			t.Errorf("a low-degree node (%d) survived the cap", n.Deg)
		}
	}
}

// TestAPINoteMove: POST .../move relocates a note into a folder (id/handle
// unchanged), edit+CSRF gated, and rejects traversal folders.
func TestAPITaskEdit(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	id := addTask(t, s, "fix the bug @backend +api")

	// gated: no CSRF → 403; empty text → 400.
	if code, _ := postForm(s, "/api/tasks/"+id, "", mustValues("text", "x")); code != 403 {
		t.Errorf("edit without CSRF should 403, got %d", code)
	}
	if code, _ := postForm(s, "/api/tasks/"+id, s.csrf, mustValues("text", "   ")); code != 400 {
		t.Errorf("empty text should 400, got %d", code)
	}

	// Edit the description; the id is preserved and the inline tags re-parse.
	code, body := postForm(s, "/api/tasks/"+id, s.csrf, mustValues("text", "fix the AUTH bug @security +api"))
	if code != 200 {
		t.Fatalf("edit: %d %s", code, body)
	}
	tk := mustDoc(t, s).FindByID(id)
	if tk == nil {
		t.Fatal("task vanished after edit (id must be preserved)")
	}
	if !strings.Contains(tk.Text, "AUTH") {
		t.Errorf("edit should update the description, got %q", tk.Text)
	}
	if !contains(tk.Tags(), "security") || contains(tk.Tags(), "backend") {
		t.Errorf("inline tags should re-parse (security in, backend out), got %v", tk.Tags())
	}
}

// TestAPITaskReschedule: the edit endpoint also takes due/pri on their own —
// natural-language values resolved server-side, "none" clearing the field, and
// the description left untouched (the quick-reschedule wire contract).
func TestAPITaskReschedule(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	id := addTask(t, s, "renew cert @ops")

	// A due-only update must not require text.
	code, body := postForm(s, "/api/tasks/"+id, s.csrf, mustValues("due", "tomorrow"))
	if code != 200 {
		t.Fatalf("due-only edit: %d %s", code, body)
	}
	tk := mustDoc(t, s).FindByID(id)
	wantDue := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if tk.Due() != wantDue {
		t.Errorf("due = %q, want %q (NL 'tomorrow' resolved server-side)", tk.Due(), wantDue)
	}
	if tk.Text != "renew cert @ops" {
		t.Errorf("description must be untouched by a reschedule, got %q", tk.Text)
	}

	// "none" clears; priority sets and clears the same way.
	if code, body := postForm(s, "/api/tasks/"+id, s.csrf, mustValues("due", "none", "pri", "high")); code != 200 {
		t.Fatalf("clear-due+set-pri: %d %s", code, body)
	}
	tk = mustDoc(t, s).FindByID(id)
	if tk.Due() != "" {
		t.Errorf("due should be cleared, got %q", tk.Due())
	}
	if tk.Priority != 'A' {
		t.Errorf("priority = %q, want A", tk.Priority)
	}
	if code, _ := postForm(s, "/api/tasks/"+id, s.csrf, mustValues("pri", "none")); code != 200 {
		t.Fatal("clear-pri failed")
	}
	if tk = mustDoc(t, s).FindByID(id); tk.Priority != 0 {
		t.Errorf("priority should be cleared, got %q", tk.Priority)
	}

	// Garbage is rejected before any write; nothing-at-all is a 400 too.
	if code, _ := postForm(s, "/api/tasks/"+id, s.csrf, mustValues("due", "notaday")); code != 400 {
		t.Errorf("bad due should 400, got %d", code)
	}
	if code, _ := postForm(s, "/api/tasks/"+id, s.csrf, nil); code != 400 {
		t.Errorf("no fields should 400, got %d", code)
	}
}

// TestAPIViews: GET /api/views lists the saved smart views, and GET
// /api/tasks?view=<name> applies one through view.Apply — the same code path
// as `nt view recall` — returning a single ordered group.
func TestAPIViews(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	addTask(t, s, "fix auth bug @backend")
	addTask(t, s, "write docs @docs")
	id := addTask(t, s, "old backend chore @backend")
	if code, _ := postForm(s, "/api/tasks/"+id+"/done", s.csrf, nil); code != 200 {
		t.Fatal("done failed")
	}

	// No views.json yet → empty list, not an error.
	resp, body := get(t, s, "/api/views")
	if resp.StatusCode != 200 || !strings.Contains(body, `"views":[]`) {
		t.Errorf("empty views: %d %s", resp.StatusCode, body)
	}

	if err := view.Save(s.eng.S.Dir, map[string]view.Spec{
		"backend": {Tag: "backend", Sort: "urgency"},
	}); err != nil {
		t.Fatal(err)
	}
	resp, body = get(t, s, "/api/views")
	if resp.StatusCode != 200 || !strings.Contains(body, `"name":"backend"`) {
		t.Fatalf("views list: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "--tag backend") {
		t.Errorf("view summary should describe the filter, got %s", body)
	}

	// Apply it: one group named after the view, done + other-tag rows excluded.
	resp, body = get(t, s, "/api/tasks?view=backend")
	if resp.StatusCode != 200 {
		t.Fatalf("tasks?view: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"status":"backend"`) {
		t.Errorf("view results should come back as one group named for the view: %s", body)
	}
	if !strings.Contains(body, "fix auth bug") || strings.Contains(body, "write docs") {
		t.Errorf("view should keep @backend and drop @docs: %s", body)
	}
	if strings.Contains(body, "old backend chore") {
		t.Errorf("default visibility hides done tasks: %s", body)
	}

	if r, _ := get(t, s, "/api/tasks?view=nope"); r.StatusCode != 404 {
		t.Errorf("unknown view should 404, got %d", r.StatusCode)
	}
}

// TestAPIUndo: POST /api/undo reverts the latest write through the
// transactional engine (the toast's "Undo" wire), is gated like every write,
// and 409s when there's nothing to undo.
func TestAPIUndo(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true

	// Nothing journaled yet → 409, not a silent no-op.
	if code, _ := postForm(s, "/api/undo", s.csrf, nil); code != 409 {
		t.Errorf("undo on empty journal should 409, got %d", code)
	}

	id := addTask(t, s, "ship the thing")
	if code, _ := postForm(s, "/api/tasks/"+id+"/done", s.csrf, nil); code != 200 {
		t.Fatal("done failed")
	}
	if st := mustDoc(t, s).FindByID(id).Status(); st != "done" {
		t.Fatalf("precondition: status = %q, want done", st)
	}

	// Gate: no CSRF → 403.
	if code, _ := postForm(s, "/api/undo", "", nil); code != 403 {
		t.Errorf("undo without CSRF should 403, got %d", code)
	}

	// Undo the completion; the response carries the fresh groups.
	code, body := postForm(s, "/api/undo", s.csrf, nil)
	if code != 200 {
		t.Fatalf("undo: %d %s", code, body)
	}
	if st := mustDoc(t, s).FindByID(id).Status(); st == "done" {
		t.Errorf("undo should reopen the task, still %q", st)
	}
	if !strings.Contains(body, `"groups"`) {
		t.Errorf("undo should respond with the task groups, got %s", body)
	}
}

func TestAPINoteMove(t *testing.T) {
	s := newTestServer(t)
	target, _ := note.Create(s.eng.S, "Target", "body", nil, "cli", "")
	note.Create(s.eng.S, "Linker", "see [[Target]]", nil, "cli", "")

	// gated off read-only
	if code, _ := postForm(s, "/api/notes/"+target.ID+"/move", "", mustValues("folder", "work")); code != 403 {
		t.Errorf("move should 403 read-only, got %d", code)
	}
	s.allowEdit = true

	code, body := postForm(s, "/api/notes/"+target.ID+"/move", s.csrf, mustValues("folder", "work/auth"))
	if code != 200 {
		t.Fatalf("move: %d %s", code, body)
	}
	res := decode[apitypes.MovedNote](t, body)
	if res.Rel != "work/auth/target.md" {
		t.Errorf("new rel = %q, want work/auth/target.md", res.Rel)
	}
	if res.Handle != target.ID {
		t.Errorf("handle/URL must be stable across a move: %q vs %q", res.Handle, target.ID)
	}
	notes, _ := note.List(s.eng.S)
	var moved *note.Note
	for _, n := range notes {
		if n.Title == "Target" {
			moved = n
		}
	}
	if moved == nil || moved.Rel != "work/auth/target.md" {
		t.Fatalf("note should now live at work/auth/, got %+v", moved)
	}

	// traversal folder rejected
	if code, _ := postForm(s, "/api/notes/"+target.ID+"/move", s.csrf, mustValues("folder", "../etc")); code != 400 {
		t.Errorf("traversal folder should 400, got %d", code)
	}
}

func TestAPIReview(t *testing.T) {
	s := newTestServer(t)
	addTask(t, s, "pay invoice due:2020-01-01") // long overdue
	addTask(t, s, "vague idea")                 // no due date

	_, body := get(t, s, "/api/review")
	rev := decode[apitypes.ReviewResponse](t, body)

	if len(rev.Overdue) != 1 || !strings.Contains(rev.Overdue[0].Text, "pay invoice") {
		t.Errorf("overdue bucket should hold the past-due task, got %+v", rev.Overdue)
	}
	if len(rev.Undated) != 1 || !strings.Contains(rev.Undated[0].Text, "vague idea") {
		t.Errorf("undated bucket should hold the no-due task, got %+v", rev.Undated)
	}
	if rev.StaleDays != 14 {
		t.Errorf("staleDays = %d, want 14", rev.StaleDays)
	}
}

func TestAPINotesGrid(t *testing.T) {
	s := newTestServer(t)
	long := strings.Repeat("alpha beta gamma ", 40) // ~680 chars, forces truncation
	note.Create(s.eng.S, "Auth", "# Auth\n\n- "+long, []string{"spec"}, "cli", "work")
	note.Create(s.eng.S, "Standup", "# Standup\n\nnotes", nil, "cli", "daily")
	note.Create(s.eng.S, "Loose", "# Loose\n\nat the root", nil, "cli", "")

	_, body := get(t, s, "/api/notes/grid")
	grid := decode[apitypes.NotesGrid](t, body)

	if len(grid.Notes) != 3 {
		t.Fatalf("want 3 cards, got %d: %+v", len(grid.Notes), grid.Notes)
	}
	// Distinct folders only, sorted; the root note contributes none.
	if got := strings.Join(grid.Folders, ","); got != "daily,work" {
		t.Errorf("folders = %q, want daily,work", got)
	}

	byTitle := map[string]apitypes.NoteCard{}
	for _, c := range grid.Notes {
		byTitle[c.Title] = c
	}
	auth := byTitle["Auth"]
	if auth.Folder != "work" || auth.Handle == "" || auth.URL == "" {
		t.Errorf("Auth card wrong: %+v", auth)
	}
	if len(auth.Tags) != 1 || auth.Tags[0] != "spec" {
		t.Errorf("Auth tags = %v, want [spec]", auth.Tags)
	}
	// Preview skips the "# Auth" heading + list marker, and is capped with an ellipsis.
	if strings.Contains(auth.Preview, "#") || !strings.HasPrefix(auth.Preview, "alpha beta") {
		t.Errorf("preview should start at the body text, got %q", auth.Preview)
	}
	if !strings.HasSuffix(auth.Preview, "…") || len([]rune(auth.Preview)) > 182 {
		t.Errorf("long preview should be truncated with …, got %d runes: %q", len([]rune(auth.Preview)), auth.Preview)
	}
	if byTitle["Loose"].Folder != "" {
		t.Errorf("root note folder should be empty, got %q", byTitle["Loose"].Folder)
	}
}

// TestAPITaskBulk: bulk done/delete/due apply to many tasks in ONE transaction,
// so a single undo reverts the whole batch (the engine's undo is single-level).
func TestAPITaskBulk(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	a := addTask(t, s, "alpha")
	b := addTask(t, s, "beta")
	c := addTask(t, s, "gamma")

	// gated
	if code, _ := postForm(s, "/api/tasks/bulk", "", mustValues("action", "done", "ids", a)); code != 403 {
		t.Errorf("bulk without CSRF should 403, got %d", code)
	}

	// bulk done a+b in one transaction
	code, body := postForm(s, "/api/tasks/bulk", s.csrf, mustValues("action", "done", "ids", a+","+b))
	if code != 200 {
		t.Fatalf("bulk done: %d %s", code, body)
	}
	if !mustDoc(t, s).FindByID(a).Done || !mustDoc(t, s).FindByID(b).Done {
		t.Fatal("both a and b should be done")
	}
	if mustDoc(t, s).FindByID(c).Done {
		t.Fatal("c should be untouched")
	}

	// ONE undo reverts the whole batch
	if code, _ := postForm(s, "/api/undo", s.csrf, nil); code != 200 {
		t.Fatal("undo failed")
	}
	if mustDoc(t, s).FindByID(a).Done || mustDoc(t, s).FindByID(b).Done {
		t.Errorf("one undo should reopen the whole bulk batch")
	}

	// bulk due, NL resolved server-side, on a+b+c
	code, body = postForm(s, "/api/tasks/bulk", s.csrf, mustValues("action", "due", "ids", a+","+b+","+c, "due", "tomorrow"))
	if code != 200 {
		t.Fatalf("bulk due: %d %s", code, body)
	}
	want := timeNowPlusISO(1)
	for _, id := range []string{a, b, c} {
		if got := mustDoc(t, s).FindByID(id).Due(); got != want {
			t.Errorf("bulk due: %s due=%q want %q", id, got, want)
		}
	}

	// unknown ids are skipped, not fatal; an all-unknown batch 400s (errNoTask)
	if code, _ := postForm(s, "/api/tasks/bulk", s.csrf, mustValues("action", "done", "ids", "nope,"+c)); code != 200 {
		t.Errorf("a batch with one good id should apply, got %d", code)
	}
	if code, _ := postForm(s, "/api/tasks/bulk", s.csrf, mustValues("action", "done", "ids", "nope1,nope2")); code != 400 {
		t.Errorf("all-unknown batch should 400, got %d", code)
	}
	// bad action / bad due
	if code, _ := postForm(s, "/api/tasks/bulk", s.csrf, mustValues("action", "frobnicate", "ids", a)); code != 400 {
		t.Errorf("unknown action should 400, got %d", code)
	}
}

func timeNowPlusISO(days int) string {
	return time.Now().AddDate(0, 0, days).Format("2006-01-02")
}

// TestAPINoteTags: add/remove tags edits frontmatter only — the body and
// unmodeled frontmatter keys survive (note.Save preserves Extra).
func TestAPINoteTags(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	n, _ := note.Create(s.eng.S, "Doc", "# Doc\n\nbody text", []string{"alpha"}, "cli", "")

	// gated
	if code, _ := postForm(s, "/api/notes/"+n.ID+"/tags", "", mustValues("add", "beta")); code != 403 {
		t.Errorf("tags without CSRF should 403, got %d", code)
	}
	// neither add nor remove → 400
	if code, _ := postForm(s, "/api/notes/"+n.ID+"/tags", s.csrf, nil); code != 400 {
		t.Errorf("empty tag edit should 400, got %d", code)
	}

	// add two (one with a @, deduped against existing), remove the original
	code, body := postForm(s, "/api/notes/"+n.ID+"/tags", s.csrf, mustValues("add", "@beta, gamma", "remove", "alpha"))
	if code != 200 {
		t.Fatalf("tags: %d %s", code, body)
	}
	res := decode[apitypes.NoteTags](t, body)
	if contains(res.Tags, "alpha") || !contains(res.Tags, "beta") || !contains(res.Tags, "gamma") {
		t.Errorf("tags after edit = %v, want [beta gamma]", res.Tags)
	}

	// reload from disk: body intact, tags persisted
	fresh, err := note.Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fresh.Body, "body text") {
		t.Errorf("body should be untouched, got %q", fresh.Body)
	}
	if contains(fresh.Tags, "alpha") || !contains(fresh.Tags, "beta") {
		t.Errorf("persisted tags = %v", fresh.Tags)
	}
}

// TestAPITaskNote: a task's "body" is a linked note. POST .../note creates a
// detail note titled from the task and links it; the task row then exposes the
// note URL and drops the raw [[link]] from its displayed text. Idempotent.
func TestAPITaskNote(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	id := addTask(t, s, "design the auth flow @security")

	// gated
	if code, _ := postForm(s, "/api/tasks/"+id+"/note", "", nil); code != 403 {
		t.Errorf("note without CSRF should 403, got %d", code)
	}

	code, body := postForm(s, "/api/tasks/"+id+"/note", s.csrf, nil)
	if code != 200 {
		t.Fatalf("add note: %d %s", code, body)
	}
	res := decode[apitypes.CreatedNote](t, body)
	if res.URL == "" {
		t.Fatal("expected a note URL")
	}

	// The note exists, titled from the task's clean text.
	notes, _ := note.List(s.eng.S)
	var made *note.Note
	for _, n := range notes {
		if n.Title == "design the auth flow" {
			made = n
		}
	}
	if made == nil {
		t.Fatalf("detail note not created with the task's title; have %d notes", len(notes))
	}
	if !strings.HasPrefix(made.Rel, note.TaskNoteFolder+"/") {
		t.Errorf("task note should be filed under %s/, got rel %q", note.TaskNoteFolder, made.Rel)
	}

	// The task now links it, and the row resolves it + hides the raw [[link]].
	tk := mustDoc(t, s).FindByID(id)
	if !strings.Contains(tk.Text, "[[design the auth flow]]") {
		t.Errorf("task should link the note, text=%q", tk.Text)
	}
	_, tbody := get(t, s, "/api/tasks")
	tr := decode[apitypes.TasksResponse](t, tbody)
	var row *apitypes.Task
	for gi := range tr.Groups {
		for ri := range tr.Groups[gi].Tasks {
			if tr.Groups[gi].Tasks[ri].ID == id {
				row = &tr.Groups[gi].Tasks[ri]
			}
		}
	}
	if row == nil {
		t.Fatal("task row missing")
	}
	if row.NoteURL == "" || row.NoteTitle != "design the auth flow" {
		t.Errorf("row should expose the linked note: url=%q title=%q", row.NoteURL, row.NoteTitle)
	}
	if strings.Contains(row.Text, "[[") {
		t.Errorf("the raw [[link]] should be stripped from the displayed text, got %q", row.Text)
	}

	// Idempotent: a second call returns the same note, no duplicate.
	code, body2 := postForm(s, "/api/tasks/"+id+"/note", s.csrf, nil)
	if code != 200 {
		t.Fatalf("second add: %d", code)
	}
	if decode[apitypes.CreatedNote](t, body2).URL != res.URL {
		t.Error("a second add-note should return the existing note, not create another")
	}
	notes2, _ := note.List(s.eng.S)
	if len(notes2) != len(notes) {
		t.Errorf("no new note should be created on the second call: %d → %d", len(notes), len(notes2))
	}
}

// TestArchivedHiddenFromTagsAndGraph: an archived note must stay out of the
// discovery surfaces — the tag cloud and the graph — like it already is for the
// sidebar, search, and orphans (and as the note page's banner promises).
func TestAPINoteFavorite(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Cheat Sheet", "commands", nil, "cli", "")

	// Gated off when read-only (mirrors archive/move).
	if code, _ := postForm(s, "/api/notes/"+n.ID+"/favorite", "", mustValues("favorite", "true")); code != 403 {
		t.Errorf("favorite should 403 read-only, got %d", code)
	}
	s.allowEdit = true

	// Star it.
	code, body := postForm(s, "/api/notes/"+n.ID+"/favorite", s.csrf, mustValues("favorite", "true"))
	if code != 200 {
		t.Fatalf("favorite: %d %s", code, body)
	}
	res := decode[apitypes.FavoritedNote](t, body)
	if !res.Favorite || res.Handle != n.ID {
		t.Errorf("favorite result = %+v, want favorite=true handle=%q", res, n.ID)
	}

	// It persists to disk and surfaces on the note view + grid card.
	if reloaded, _ := note.Load(n.Path); reloaded == nil || !reloaded.Favorite {
		t.Errorf("favorite: true should persist to the note file")
	}
	_, body = get(t, s, "/api/notes/"+n.ID)
	if !decode[apitypes.NoteView](t, body).Favorite {
		t.Error("note view should report favorite=true")
	}
	_, body = get(t, s, "/api/notes/grid")
	grid := decode[apitypes.NotesGrid](t, body)
	var starred bool
	for _, c := range grid.Notes {
		if c.Handle == n.ID {
			starred = c.Favorite
		}
	}
	if !starred {
		t.Error("grid card should report favorite=true")
	}

	// Unstar (explicit false) clears it.
	if code, _ := postForm(s, "/api/notes/"+n.ID+"/favorite", s.csrf, mustValues("favorite", "false")); code != 200 {
		t.Fatalf("unfavorite should 200, got %d", code)
	}
	if reloaded, _ := note.Load(n.Path); reloaded.Favorite {
		t.Error("unfavorite should clear the flag")
	}
}

func TestArchivedHiddenFromTagsAndGraph(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	note.Create(s.eng.S, "Live", "active", []string{"shared"}, "cli", "")
	old, _ := note.Create(s.eng.S, "Retired", "old", []string{"shared", "stale"}, "cli", "")
	addTask(t, s, "follow up on [[Retired]]") // a task linking the archived note

	// Archive "Retired" (frontmatter flag, the same write apiNoteArchive does).
	fresh, err := note.Load(old.Path)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Archived = true
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}

	// Tags: only the live note counts; "stale" (archived-only) is gone, "shared" = 1.
	_, body := get(t, s, "/api/tags")
	for _, tg := range decode[apitypes.TagsResponse](t, body).Tags {
		if tg.Name == "stale" {
			t.Errorf("archived-only tag %q should not appear in the cloud", tg.Name)
		}
		if tg.Name == "shared" && tg.Count != 1 {
			t.Errorf("shared count should drop archived: got %d, want 1", tg.Count)
		}
	}

	// Graph: no node for the archived note (and the task linking only it is omitted).
	_, body = get(t, s, "/api/graph")
	g := decode[apitypes.GraphData](t, body)
	for _, n := range g.Nodes {
		if n.Title == "Retired" {
			t.Errorf("archived note should not be a graph node: %+v", g.Nodes)
		}
		if n.Kind == "task" {
			t.Errorf("a task linking only an archived note should not appear: %+v", n)
		}
	}
}
