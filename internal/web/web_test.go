package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// TestIdlessNoteRoutesByPath: a note authored outside nt (no id: frontmatter,
// e.g. from Obsidian) must still be browsable — routed by its path, not the
// broken "/n/".
func TestIdlessNoteRoutesByPath(t *testing.T) {
	s := newTestServer(t)
	dir := filepath.Join(s.eng.S.NotesDir(), "deep", "nested")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte("# Big Doc\n\nbody here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, idx := get(t, s, "/"); !strings.Contains(idx, `/n/deep%2Fnested%2Fbig.md`) {
		t.Fatalf("id-less note not path-routed in tree:\n%s", idx)
	}
	resp, body := get(t, s, "/n/deep%2Fnested%2Fbig.md")
	if resp.StatusCode != 200 || !strings.Contains(body, ">Big Doc<") {
		t.Fatalf("id-less note page didn't load: status=%d", resp.StatusCode)
	}
}

// TestSnapshotLinkGraph: the read-model precomputes note→note backlinks, the
// task→note reference panel, forward adjacency, and the orphan ("linked") set —
// the maps the per-request ripgrep used to recompute.
func TestSnapshotLinkGraph(t *testing.T) {
	s := newTestServer(t)
	b, _ := note.Create(s.eng.S, "Target", "the target note", nil, "cli", "")
	a, _ := note.Create(s.eng.S, "Source", "see [[Target]] for context", nil, "cli", "")
	if err := s.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		tk := task.New("wire up [[Target]]")
		d.Append(tk)
		rec.Added(tk)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	snap := buildSnapshot(s.eng)
	if got := snap.backlinks[b.Path]; len(got) != 1 || got[0].Title != "Source" {
		t.Fatalf("backlinks[Target] = %+v, want one from Source", got)
	}
	if got := snap.taskRefs[b.Path]; len(got) != 1 || !strings.Contains(got[0].Text, "wire up") {
		t.Fatalf("taskRefs[Target] = %+v, want the linking task", got)
	}
	if !contains(snap.fwd[a.Path], b.Path) {
		t.Fatalf("fwd[Source] = %v, want it to include Target", snap.fwd[a.Path])
	}
	if !snap.linked[b.Path] {
		t.Error("Target should be linked (not an orphan)")
	}
	if snap.linked[a.Path] {
		t.Error("Source has no inbound links — should be an orphan")
	}
}

func postForm(s *Server, path, csrf string, form url.Values) (int, string) {
	rec := httptest.NewRecorder()
	var body *strings.Reader
	r := httptest.NewRequest("POST", path, strings.NewReader(""))
	if form != nil {
		body = strings.NewReader(form.Encode())
		r = httptest.NewRequest("POST", path, body)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF", csrf)
	}
	s.routes().ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// addTask appends a task through the engine and returns its ULID.
func addTask(t *testing.T, s *Server, text string) string {
	t.Helper()
	var id string
	if err := s.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		tk := task.New(text)
		d.Append(tk)
		rec.Added(tk)
		id = tk.ID()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTaskActionsGated(t *testing.T) {
	s := newTestServer(t) // read-only
	id := addTask(t, s, "guard me")
	if code, _ := postForm(s, "/tasks/"+id+"/done", "", nil); code != 403 {
		t.Errorf("done without --edit should 403, got %d", code)
	}
	s.allowEdit = true
	if code, _ := postForm(s, "/tasks/"+id+"/done", "", nil); code != 403 {
		t.Errorf("done without CSRF should 403, got %d", code)
	}
	if code, _ := postForm(s, "/tasks/"+id+"/done", "wrong", nil); code != 403 {
		t.Errorf("done with bad CSRF should 403, got %d", code)
	}
}

func TestTaskDoneReopenStatusDelete(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	id := addTask(t, s, "ship it")

	code, body := postForm(s, "/tasks/"+id+"/done", s.csrf, nil)
	if code != 200 || !strings.Contains(body, "s-done") {
		t.Fatalf("done: code=%d body=%s", code, body)
	}
	if tk := mustDoc(t, s).FindByID(id); tk == nil || !tk.Done {
		t.Fatal("task should be done")
	}

	code, _ = postForm(s, "/tasks/"+id+"/reopen", s.csrf, nil)
	if code != 200 || mustDoc(t, s).FindByID(id).Done {
		t.Fatalf("reopen failed: code=%d", code)
	}

	code, _ = postForm(s, "/tasks/"+id+"/status", s.csrf, url.Values{"status": {"doing"}})
	if code != 200 || mustDoc(t, s).FindByID(id).State() != "doing" {
		t.Fatalf("status doing failed: code=%d", code)
	}

	code, _ = postForm(s, "/tasks/"+id+"/delete", s.csrf, nil)
	if code != 200 || mustDoc(t, s).FindByID(id) != nil {
		t.Fatalf("delete failed: code=%d", code)
	}
}

func TestTaskNew(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	code, body := postForm(s, "/tasks/new", s.csrf, url.Values{
		"text": {"write the docs"}, "pri": {"high"}, "due": {"2026-07-01"}, "project": {"site"},
	})
	if code != 200 || !strings.Contains(body, "write the docs") {
		t.Fatalf("new task: code=%d body=%s", code, body)
	}
	tasks := mustDoc(t, s).Tasks()
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	tk := tasks[0]
	if tk.Source() != "web" || tk.Due() != "2026-07-01" || len(tk.Projects()) != 1 {
		t.Fatalf("task fields wrong: src=%s due=%s proj=%v", tk.Source(), tk.Due(), tk.Projects())
	}
	// empty text is rejected
	if code, _ := postForm(s, "/tasks/new", s.csrf, url.Values{"text": {"  "}}); code != 400 {
		t.Errorf("empty task should 400, got %d", code)
	}
}

func TestPreviewEndpoint(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Target", "x", nil, "cli", "")

	// gated off when read-only
	if code, _ := postBody(s, "/preview", "", "# hi"); code != 404 {
		t.Errorf("preview should 404 when read-only, got %d", code)
	}
	s.allowEdit = true
	if code, _ := postBody(s, "/preview", "", "# hi"); code != 403 {
		t.Errorf("preview without CSRF should 403, got %d", code)
	}
	// renders markdown + resolves wikilinks the same way the note page does,
	// and ignores leading frontmatter.
	code, body := postBody(s, "/preview", s.csrf, "---\nid: x\n---\n\n## Heading\n\nsee [[Target]]")
	if code != 200 {
		t.Fatalf("preview code=%d", code)
	}
	if !strings.Contains(body, "<h2") || !strings.Contains(body, `class="wikilink"`) {
		t.Fatalf("preview did not render markdown/wikilink:\n%s", body)
	}
	if strings.Contains(body, "id: x") {
		t.Errorf("preview should strip frontmatter:\n%s", body)
	}
}

// postBody POSTs a text/plain body with optional CSRF.
func postBody(s *Server, path, csrf, body string) (int, string) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "text/plain")
	if csrf != "" {
		r.Header.Set("X-CSRF", csrf)
	}
	s.routes().ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

func mustDoc(t *testing.T, s *Server) *task.Doc {
	t.Helper()
	d, err := s.eng.Read()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestWriteTrackerSelfWrite(t *testing.T) {
	wt := newWriteTracker()
	if wt.isSelf("/x") {
		t.Error("unmarked path should not be a self-write")
	}
	wt.mark("/x")
	if !wt.isSelf("/x") {
		t.Error("freshly-marked path should be a self-write")
	}
	if wt.isSelf("/y") {
		t.Error("a different path should not be a self-write")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("NT_DIR", t.TempDir())
	eng, err := mutate.Open()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(eng, "test")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(t *testing.T, s *Server, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Result(), rec.Body.String()
}

func TestRenderBodyWikilinksAndMermaid(t *testing.T) {
	s := newTestServer(t)
	if _, err := note.Create(s.eng.S, "Other Note", "", nil, "cli", ""); err != nil {
		t.Fatal(err)
	}
	doc, notes := s.load()
	body := "see [[other-note]] and [[ghost]]\n\n```mermaid\ngraph TD; A-->B\n```\n"
	html, err := renderBody(body, doc, notes)
	if err != nil {
		t.Fatal(err)
	}
	h := string(html)
	if !strings.Contains(h, `<a class="wikilink" href="/n/`) {
		t.Errorf("resolved wikilink not classed:\n%s", h)
	}
	if !strings.Contains(h, `?missing=1">ghost</a>`) {
		t.Errorf("broken wikilink not marked missing:\n%s", h)
	}
	if !strings.Contains(h, `<div class="mermaid">graph TD`) {
		t.Errorf("mermaid fence not converted to div:\n%s", h)
	}
}

func TestMermaidNotLinkifiedInFence(t *testing.T) {
	s := newTestServer(t)
	doc, notes := s.load()
	// A [[x]] inside a code fence must NOT become a link.
	html, _ := renderBody("```\nsee [[x]] here\n```\n", doc, notes)
	if strings.Contains(string(html), "/n/") {
		t.Errorf("wikilink inside code fence was linkified:\n%s", html)
	}
}

func TestIndexShowsFolderTree(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "Auth", "", nil, "cli", "work")
	_, _ = note.Create(s.eng.S, "Scratch", "", nil, "cli", "")
	resp, body := get(t, s, "/")
	if resp.StatusCode != 200 {
		t.Fatalf("index status %d", resp.StatusCode)
	}
	if !strings.Contains(body, "<summary>work</summary>") {
		t.Errorf("folder not in tree:\n%s", body)
	}
	if !strings.Contains(body, "Scratch") {
		t.Error("root note missing from tree")
	}
}

func TestNotePageAndKeyRedirect(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "JWT Expiry", "tokens last 24h", nil, "cli", "")

	// by stable id
	resp, body := get(t, s, "/n/"+n.ID)
	if resp.StatusCode != 200 || !strings.Contains(body, "JWT Expiry") {
		t.Fatalf("note page status=%d", resp.StatusCode)
	}
	if !strings.Contains(body, "tokens last 24h") {
		t.Error("note body not rendered")
	}

	// by key → 302 redirect to the id URL
	resp2, _ := get(t, s, "/n/jwt-expiry")
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("key lookup should redirect, got %d", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); !strings.Contains(loc, n.ID) {
		t.Errorf("redirect to wrong place: %q", loc)
	}
}

func TestBacklinksRendered(t *testing.T) {
	s := newTestServer(t)
	target, _ := note.Create(s.eng.S, "Target", "body", nil, "cli", "")
	_, _ = note.Create(s.eng.S, "Source", "see [[target]]", nil, "cli", "")
	_, body := get(t, s, "/n/"+target.ID)
	if !strings.Contains(body, `>Source</a>`) {
		t.Errorf("backlink to Source not shown:\n%s", body)
	}
}

func TestSearch(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "Race Conditions", "mutex and locks", nil, "cli", "")
	_, body := get(t, s, "/search?q=mutex")
	if !strings.Contains(body, `class="results"`) || !strings.Contains(body, "Race Conditions") {
		t.Errorf("search did not surface the note:\n%s", body)
	}
}

func TestNotFoundState(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/n/nope?missing=1")
	if !strings.Contains(body, "Note not found") {
		t.Errorf("missing route should show not-found state:\n%s", body)
	}
}

func TestAmbiguousCandidates(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "Auth", "", nil, "cli", "work")
	_, _ = note.Create(s.eng.S, "Auth", "", nil, "cli", "personal")
	_, body := get(t, s, "/n/auth?missing=1")
	if !strings.Contains(body, "Ambiguous link") {
		t.Errorf("colliding name should be ambiguous:\n%s", body)
	}
	if strings.Count(body, `<code>`) < 2 { // two candidate paths listed
		t.Errorf("expected candidate list:\n%s", body)
	}
}

func TestNoteMetadataAndBreadcrumb(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Auth Design", "body", nil, "claude", "work/auth")
	_, body := get(t, s, "/n/"+n.ID)
	if !strings.Contains(body, "src-badge--claude") || !strings.Contains(body, ">claude<") {
		t.Errorf("src:claude badge missing:\n%s", body)
	}
	if !strings.Contains(body, `class="crumbs"`) || !strings.Contains(body, ">work<") || !strings.Contains(body, ">auth<") {
		t.Errorf("breadcrumb missing folder segments:\n%s", body)
	}
	if !strings.Contains(body, `class="skip-link"`) {
		t.Error("skip-link missing")
	}
}

func TestReferencedByTasksPanel(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Architecture", "the design", nil, "claude", "")
	// a task that links to the note
	if err := s.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		tk := task.New("implement [[architecture]] @backend")
		d.Append(tk)
		rec.Added(tk)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/n/"+n.ID)
	if !strings.Contains(body, "Referenced by tasks") {
		t.Fatalf("task-refs panel missing:\n%s", body)
	}
	if !strings.Contains(body, "implement") || !strings.Contains(body, "s-open") {
		t.Errorf("task ref text/status missing:\n%s", body)
	}
}

func TestBacklinkSnippet(t *testing.T) {
	s := newTestServer(t)
	target, _ := note.Create(s.eng.S, "Target", "body", nil, "cli", "")
	_, _ = note.Create(s.eng.S, "Source", "context before [[target]] context after", nil, "cli", "")
	_, body := get(t, s, "/n/"+target.ID)
	if !strings.Contains(body, "backlinks__snippet") || !strings.Contains(body, "context before") {
		t.Errorf("backlink snippet missing:\n%s", body)
	}
}

func TestStaticAssets(t *testing.T) {
	s := newTestServer(t)
	if resp, _ := get(t, s, "/static/style.css"); resp.StatusCode != 200 ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Errorf("style.css served wrong: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp, _ := get(t, s, "/static/mermaid.min.js")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("mermaid should be gzip-encoded: %d %q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
	}
	if r, body := get(t, s, "/static/htmx.min.js"); r.StatusCode != 200 || !strings.Contains(body, "htmx") {
		t.Errorf("htmx not served: %d", r.StatusCode)
	}
}

func TestTagsPage(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "A", "", []string{"auth", "ref"}, "cli", "")
	_, _ = note.Create(s.eng.S, "B", "", []string{"auth"}, "cli", "")
	_, body := get(t, s, "/tags")
	if !strings.Contains(body, `href="/search?tag=auth"`) || !strings.Contains(body, `class="tag__count">2`) {
		t.Fatalf("tags page wrong:\n%s", body)
	}
}

func TestSearchTagFilterJSON(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "Auth Design", "", []string{"auth"}, "cli", "")
	_, _ = note.Create(s.eng.S, "Other Note", "", []string{"misc"}, "cli", "")
	resp, body := get(t, s, "/search?json=1&tag=auth")
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("json search content-type %q", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "Auth Design") || strings.Contains(body, "Other Note") {
		t.Fatalf("tag filter wrong: %s", body)
	}
}

func TestTasksDashboard(t *testing.T) {
	s := newTestServer(t)
	if err := s.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		o := task.New("open thing")
		d.Append(o)
		rec.Added(o)
		dn := task.New("done thing")
		dn.SetDone(true, "2026-01-01")
		d.Append(dn)
		rec.Added(dn)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/tasks")
	if !strings.Contains(body, "taskgroup") || !strings.Contains(body, "s-open") || !strings.Contains(body, "s-done") {
		t.Fatalf("tasks dashboard missing groups:\n%s", body)
	}
}

func TestPaletteIndexAndOnboarding(t *testing.T) {
	s := newTestServer(t)
	if _, body := get(t, s, "/"); !strings.Contains(body, "No notes yet") {
		t.Fatalf("empty onboarding missing:\n%s", body)
	}
	_, _ = note.Create(s.eng.S, "Findme", "", nil, "cli", "")
	_, body := get(t, s, "/")
	if !strings.Contains(body, `id="nt-notes"`) || !strings.Contains(body, `id="palette"`) || !strings.Contains(body, "Findme") {
		t.Fatalf("palette/note-index missing:\n%s", body)
	}
}

func TestSiblingPager(t *testing.T) {
	s := newTestServer(t)
	a, _ := note.Create(s.eng.S, "Alpha", "", nil, "cli", "ref")
	_, _ = note.Create(s.eng.S, "Beta", "", nil, "cli", "ref")
	_, body := get(t, s, "/n/"+a.ID)
	if !strings.Contains(body, `class="pager"`) || !strings.Contains(body, `class="pager__next"`) {
		t.Fatalf("sibling pager missing:\n%s", body)
	}
}

func TestSyntaxHighlight(t *testing.T) {
	s := newTestServer(t)
	doc, notes := s.load()
	html, _ := renderBody("```go\nfunc main() {}\n```\n", doc, notes)
	if !strings.Contains(string(html), `class="chroma"`) || !strings.Contains(string(html), `class="kd"`) {
		t.Fatalf("go code not highlighted:\n%s", html)
	}
	if plain, _ := renderBody("```nosuchlang\nx\n```\n", doc, notes); strings.Contains(string(plain), "chroma") {
		t.Errorf("unknown language should be left unhighlighted")
	}
	css := highlightCSS()
	if !strings.Contains(css, ".chroma") || !strings.Contains(css, "prefers-color-scheme") {
		t.Errorf("highlight CSS missing scoping")
	}
	if strings.Contains(css, "background-color") {
		t.Errorf("highlight CSS should not set backgrounds (keeps --bg-inset)")
	}
}

func TestNotePreview(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Token Rotation", "# Token Rotation\n\nRotates weekly. Idempotent job.", nil, "cli", "")
	resp, body := get(t, s, "/n/"+n.ID+"?preview=1")
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("preview content-type %q", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "Token Rotation") || !strings.Contains(body, "Rotates weekly") {
		t.Fatalf("preview json: %s", body)
	}
	if strings.Contains(body, `# Token`) {
		t.Error("snippet should strip the leading H1")
	}
}

func TestGraphView(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "Auth", "see [[token-rotation]]", nil, "cli", "")
	_, _ = note.Create(s.eng.S, "Token Rotation", "x", nil, "cli", "")
	_, notes := s.load()
	src := graphSource(notes)
	if !strings.Contains(src, "graph LR") || !strings.Contains(src, "-->") || !strings.Contains(src, "click n") {
		t.Fatalf("graph source wrong:\n%s", src)
	}
	if _, body := get(t, s, "/graph"); !strings.Contains(body, `class="graphview"`) {
		t.Fatalf("graph page missing graphview:\n%s", body)
	}
}

func postNote(s *Server, id, csrf, body string) int {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/n/"+id, strings.NewReader(body))
	if csrf != "" {
		r.Header.Set("X-CSRF", csrf)
	}
	s.routes().ServeHTTP(rec, r)
	return rec.Code
}

func postNoteIfMatch(s *Server, id, csrf, ifMatch, body string) int {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/n/"+id, strings.NewReader(body))
	r.Header.Set("X-CSRF", csrf)
	r.Header.Set("If-Match", ifMatch)
	s.routes().ServeHTTP(rec, r)
	return rec.Code
}

func TestEditingDisabledByDefault(t *testing.T) {
	s := newTestServer(t) // allowEdit defaults false
	n, _ := note.Create(s.eng.S, "X", "body", nil, "cli", "")
	if resp, _ := get(t, s, "/n/"+n.ID+"?raw=1"); resp.StatusCode != 404 {
		t.Errorf("raw should 404 when read-only, got %d", resp.StatusCode)
	}
	if code := postNote(s, n.ID, "", "x"); code != 403 {
		t.Errorf("POST should 403 when read-only, got %d", code)
	}
	if _, body := get(t, s, "/n/"+n.ID); strings.Contains(body, `id="edit-btn"`) {
		t.Error("edit button must be hidden when read-only")
	}
}

func TestEditingSave(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	n, _ := note.Create(s.eng.S, "X", "old body here", nil, "cli", "")
	resp, raw := get(t, s, "/n/"+n.ID+"?raw=1")
	if resp.StatusCode != 200 || !strings.Contains(raw, "old body here") {
		t.Fatalf("raw not served: %d", resp.StatusCode)
	}
	if code := postNote(s, n.ID, "", "x"); code != 403 {
		t.Errorf("POST without CSRF should 403, got %d", code)
	}
	if code := postNote(s, n.ID, "wrong", "x"); code != 403 {
		t.Errorf("POST with wrong CSRF should 403, got %d", code)
	}
	if code := postNote(s, n.ID, s.csrf, "---\nid: keep\n---\n\nbrand new body\n"); code != 204 {
		t.Fatalf("POST with CSRF should 204, got %d", code)
	}
	b, _ := os.ReadFile(n.Path)
	if !strings.Contains(string(b), "brand new body") || !strings.Contains(string(b), "id: keep") {
		t.Fatalf("file not updated as written:\n%s", b)
	}
}

// TestEditingLostUpdateGuard: when a note changes on disk after the editor
// opened it (a concurrent agent/CLI write), a save carrying the stale ETag as
// If-Match is refused with 409 instead of clobbering the other write. A save
// with the current ETag goes through.
func TestEditingLostUpdateGuard(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	n, _ := note.Create(s.eng.S, "X", "v1 body", nil, "cli", "")

	resp, _ := get(t, s, "/n/"+n.ID+"?raw=1")
	stale := resp.Header.Get("ETag")
	if stale == "" {
		t.Fatal("raw response missing ETag")
	}

	// Another writer (agent/CLI) rewrites the same note underneath the editor.
	fresh := "---\nid: " + n.ID + "\n---\n\nagent wrote this\n"
	if err := store.WriteAtomic(n.Path, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}

	// The stale save is refused and the agent's write survives intact.
	if code := postNoteIfMatch(s, n.ID, s.csrf, stale, "clobbered\n"); code != 409 {
		t.Fatalf("stale save should 409, got %d", code)
	}
	if b, _ := os.ReadFile(n.Path); !strings.Contains(string(b), "agent wrote this") {
		t.Fatalf("agent write was clobbered:\n%s", b)
	}

	// Re-opening yields the current ETag, which lets the save through.
	resp2, _ := get(t, s, "/n/"+n.ID+"?raw=1")
	if code := postNoteIfMatch(s, n.ID, s.csrf, resp2.Header.Get("ETag"), "---\nid: "+n.ID+"\n---\n\nmerged\n"); code != 204 {
		t.Fatalf("fresh save should 204, got %d", code)
	}
}
