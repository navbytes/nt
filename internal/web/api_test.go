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
