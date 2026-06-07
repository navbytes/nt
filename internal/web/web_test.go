package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

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
}
