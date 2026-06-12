package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// TestSnapshotLinkGraph: the read-model precomputes note→note backlinks, the
// task→note reference panel, forward adjacency, and the orphan ("linked") set.
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

	snap := buildSnapshot(s.eng, s.notes)
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

// TestRenderExternalLinksOpenOutside: absolute http(s) links get
// target=_blank + noopener so they leave the app (a new tab in the browser, the
// system browser in the desktop webview, which has no back button); internal
// links (/n/…, #anchors) keep SPA navigation.
func TestRenderExternalLinksOpenOutside(t *testing.T) {
	s := newTestServer(t)
	if _, err := note.Create(s.eng.S, "Other Note", "", nil, "cli", ""); err != nil {
		t.Fatal(err)
	}
	doc, notes := s.load()
	body := "[ext](https://example.com/x) and [[other-note]] and [anchor](#here)\n"
	html, err := renderBody(body, doc, notes)
	if err != nil {
		t.Fatal(err)
	}
	h := string(html)
	if !strings.Contains(h, `<a href="https://example.com/x" target="_blank" rel="noopener noreferrer">`) {
		t.Errorf("external link should open outside the app:\n%s", h)
	}
	// The wikilink keeps its exact form — no target/rel injected.
	if !strings.Contains(h, `<a class="wikilink" href="/n/`) {
		t.Errorf("wikilink should render untouched:\n%s", h)
	}
	if strings.Contains(h, `href="#here" target=`) {
		t.Errorf("anchor link must NOT get target=_blank:\n%s", h)
	}
}

func TestStripTitleH1(t *testing.T) {
	cases := []struct {
		name, body, title, want string
	}{
		{"nt-prepended", "# JWT expiry handling\n\nBody text.\n", "JWT expiry handling", "Body text.\n"},
		{"leading-blanks", "\n\n# Title\n\nBody.\n", "Title", "Body.\n"},
		{"extra-hash-spaces", "#   Title\n\nBody.\n", "Title", "Body.\n"},
		{"case-insensitive", "# title\n\nBody.\n", "Title", "Body.\n"},
		{"different-heading", "# Intro\n\nBody.\n", "Title", "# Intro\n\nBody.\n"},
		{"h2-untouched", "## Title\n\nBody.\n", "Title", "## Title\n\nBody.\n"},
		{"no-heading", "Body.\n", "Title", "Body.\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripTitleH1(c.body, c.title); got != c.want {
				t.Errorf("stripTitleH1(%q, %q) = %q, want %q", c.body, c.title, got, c.want)
			}
		})
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

func TestHighlightCSSRoute(t *testing.T) {
	s := newTestServer(t)
	resp, body := get(t, s, "/static/highlight.css")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Errorf("highlight.css: status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, ".chroma") {
		t.Error("highlight.css body missing .chroma")
	}
	et := resp.Header.Get("ETag")
	if et == "" {
		t.Fatal("highlight.css missing ETag")
	}
	// Conditional request must 304.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/highlight.css", nil)
	req.Header.Set("If-None-Match", et)
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("conditional GET should 304, got %d", rec.Code)
	}
}

func TestGraphData(t *testing.T) {
	s := newTestServer(t)
	_, _ = note.Create(s.eng.S, "Auth", "see [[token-rotation]]", nil, "cli", "")
	_, _ = note.Create(s.eng.S, "Token Rotation", "x", nil, "cli", "")
	g := buildGraphData(s.current())
	if len(g.Nodes) != 2 || len(g.Links) != 1 {
		t.Fatalf("graph data wrong: %d nodes, %d links", len(g.Nodes), len(g.Links))
	}
	if g.Nodes[0].Deg != 1 || g.Nodes[1].Deg != 1 {
		t.Fatalf("degree not computed: %+v", g.Nodes)
	}
}

func TestPreviewEndpoint(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Target", "x", nil, "cli", "")

	// gated off when read-only
	if code, _ := postBody(s, "/api/preview", "", "# hi"); code != 404 {
		t.Errorf("preview should 404 when read-only, got %d", code)
	}
	s.allowEdit = true
	if code, _ := postBody(s, "/api/preview", "", "# hi"); code != 403 {
		t.Errorf("preview without CSRF should 403, got %d", code)
	}
	// renders markdown + resolves wikilinks the same way the note page does,
	// and ignores leading frontmatter.
	code, body := postBody(s, "/api/preview", s.csrf, "---\nid: x\n---\n\n## Heading\n\nsee [[Target]]")
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

func TestEditingSave(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	n, _ := note.Create(s.eng.S, "X", "old body here", nil, "cli", "")

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
// opened it, a save carrying the stale ETag as If-Match is refused with 409.
func TestEditingLostUpdateGuard(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	n, _ := note.Create(s.eng.S, "X", "v1 body", nil, "cli", "")

	// Capture the current ETag by reading the file bytes directly.
	data, _ := os.ReadFile(n.Path)
	stale := etag(data)
	if stale == "" {
		t.Fatal("raw response missing ETag")
	}

	fresh := "---\nid: " + n.ID + "\n---\n\nagent wrote this\n"
	if err := store.WriteAtomic(n.Path, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := postNoteIfMatch(s, n.ID, s.csrf, stale, "clobbered\n"); code != 409 {
		t.Fatalf("stale save should 409, got %d", code)
	}
	if b, _ := os.ReadFile(n.Path); !strings.Contains(string(b), "agent wrote this") {
		t.Fatalf("agent write was clobbered:\n%s", b)
	}
}

// ---- helpers ---------------------------------------------------------------

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

func mustDoc(t *testing.T, s *Server) *task.Doc {
	t.Helper()
	d, err := s.eng.Read()
	if err != nil {
		t.Fatal(err)
	}
	return d
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

func postNote(s *Server, id, csrf, body string) int {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/notes/"+id, strings.NewReader(body))
	if csrf != "" {
		r.Header.Set("X-CSRF", csrf)
	}
	s.routes().ServeHTTP(rec, r)
	return rec.Code
}

func postNoteIfMatch(s *Server, id, csrf, ifMatch, body string) int {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/notes/"+id, strings.NewReader(body))
	r.Header.Set("X-CSRF", csrf)
	r.Header.Set("If-Match", ifMatch)
	s.routes().ServeHTTP(rec, r)
	return rec.Code
}
