// Package web is the HTTP adapter: a localhost notes viewer (`nt web`). It is a
// read adapter over the same domain (note/links/mutate) the CLI, TUI, and MCP
// server use — so it shows exactly what they store. It is deliberately
// structured to make editing a future additive change: state lives on the
// Server struct, notes are addressed by stable id, and the render path is one
// reusable function.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/task"
)

//go:embed assets
var assetsFS embed.FS

// Server holds the shared state for the viewer. (Editing, when added, hangs
// write routes and a CSRF token here — seam #1.)
type Server struct {
	eng     *mutate.Engine
	version string
	tmpl    *template.Template
	hub     *hub
}

// NewServer parses the embedded template and prepares the viewer.
func NewServer(eng *mutate.Engine, version string) (*Server, error) {
	tmpl, err := template.ParseFS(assetsFS, "assets/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{eng: eng, version: version, tmpl: tmpl, hub: newHub()}, nil
}

// Serve opens the store and serves the viewer on addr (e.g. "127.0.0.1:0").
func Serve(version, addr string) error {
	eng, err := mutate.Open()
	if err != nil {
		return err
	}
	s, err := NewServer(eng, version)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.watch()
	fmt.Printf("nt web — serving notes at http://%s\n", ln.Addr().String())
	fmt.Println("(localhost only · live-reloads on change · Ctrl+C to stop)")
	return http.Serve(ln, s.routes()) //nolint:gosec // localhost dev server, no timeouts needed
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/n/", s.handleNote)
	mux.HandleFunc("/search", s.handleSearch)
	mux.HandleFunc("/events", s.handleSSE)
	mux.HandleFunc("/static/", s.handleStatic)
	return mux
}

func (s *Server) load() (*task.Doc, []*note.Note) {
	doc, _ := s.eng.Read()
	notes, _ := note.List(s.eng.S)
	return doc, notes
}

// ---- page model -----------------------------------------------------------

type pageData struct {
	Title       string
	Tree        []*treeNode
	ShowResults bool
	SearchQuery string
	Results     []linkRow

	IsNote      bool
	NoteTitle   string
	FolderPath  string
	FileName    string
	Crumbs      []string
	NoteSource  string
	NoteCreated string
	Tags        []string
	BodyHTML    template.HTML
	Backlinks   []Backlink
	TaskRefs    []TaskRef

	IsState    bool
	StateTitle string
	Target     string
	Candidates []linkRow
}

type linkRow struct{ URL, Title, Path string }

func (s *Server) render(w http.ResponseWriter, d *pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "layout.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ---- handlers -------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	_, notes := s.load()
	s.render(w, &pageData{Title: "nt notes", Tree: buildTree(notes, "")})
}

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	handle, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/n/"))
	if handle == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	doc, notes := s.load()
	if r.URL.Query().Get("missing") != "1" {
		if n := findByHandle(notes, handle); n != nil {
			s.renderNote(w, n, doc, notes)
			return
		}
		if it, ok := links.Resolve(handle, doc, notes); ok && it.Kind == "note" {
			if n := findByPath(notes, it.Path); n != nil {
				http.Redirect(w, r, "/n/"+url.PathEscape(noteHandle(n)), http.StatusFound)
				return
			}
		}
	}
	s.renderMissing(w, handle, notes)
}

func (s *Server) renderNote(w http.ResponseWriter, n *note.Note, doc *task.Doc, notes []*note.Note) {
	body, err := renderBody(n.Body, doc, notes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	folder, file := splitRel(n.Rel)
	var crumbs []string
	if folder != "" {
		crumbs = strings.Split(folder, "/")
	}
	s.render(w, &pageData{
		Title:       n.Title,
		Tree:        buildTree(notes, noteHandle(n)),
		IsNote:      true,
		NoteTitle:   n.Title,
		FolderPath:  folder,
		FileName:    file,
		Crumbs:      crumbs,
		NoteSource:  n.Source,
		NoteCreated: dateOnly(n.Created),
		Tags:        n.Tags,
		BodyHTML:    body,
		Backlinks:   backlinksFor(s.eng.S, n, notes),
		TaskRefs:    tasksReferencing(doc, n, notes),
	})
}

func (s *Server) renderMissing(w http.ResponseWriter, handle string, notes []*note.Note) {
	key, _ := links.NormalizeTarget(handle)
	var cands []linkRow
	for _, n := range notes {
		if links.SuffixMatch(n.Rel, key) {
			cands = append(cands, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
		}
	}
	title := "Note not found"
	if len(cands) > 1 {
		title = "Ambiguous link"
	}
	s.render(w, &pageData{
		Title:      title,
		Tree:       buildTree(notes, ""),
		IsState:    true,
		StateTitle: title,
		Target:     key,
		Candidates: cands,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	_, notes := s.load()
	if q == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	byPath := make(map[string]*note.Note, len(notes))
	for _, n := range notes {
		byPath[n.Path] = n
	}
	seen := map[string]bool{}
	var results []linkRow
	add := func(n *note.Note) {
		if n == nil || seen[noteHandle(n)] {
			return
		}
		seen[noteHandle(n)] = true
		results = append(results, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
	}
	// title matches first (cheap, most relevant), then full-text body hits.
	ql := strings.ToLower(q)
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n.Title), ql) {
			add(n)
		}
	}
	if hits, err := search.Literal(q, s.eng.S.NotesDir()); err == nil {
		for _, h := range hits {
			add(byPath[h.Path])
		}
	}
	s.render(w, &pageData{
		Title:       "Search: " + q,
		Tree:        buildTree(notes, ""),
		ShowResults: true,
		SearchQuery: q,
		Results:     results,
	})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/static/") {
	case "style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		s.writeAsset(w, "assets/style.css")
	case "app.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		s.writeAsset(w, "assets/app.js")
	case "mermaid.min.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip") // embedded pre-gzipped (~900 KB vs 3.3 MB)
		s.writeAsset(w, "assets/mermaid.min.js.gz")
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) writeAsset(w http.ResponseWriter, path string) {
	b, err := assetsFS.ReadFile(path)
	if err != nil {
		http.Error(w, "asset missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

// ---- live reload (SSE + fsnotify) -----------------------------------------

type hub struct {
	mu      sync.Mutex
	clients map[chan struct{}]bool
}

func newHub() *hub { return &hub{clients: map[chan struct{}]bool{}} }

func (h *hub) add() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.clients[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *hub) remove(ch chan struct{}) {
	h.mu.Lock()
	if h.clients[ch] {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *hub) broadcast() {
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.add()
	defer s.hub.remove(ch)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			_, _ = fmt.Fprint(w, "data: reload\n\n")
			fl.Flush()
		}
	}
}

// watch broadcasts a reload on any change under notes/ or the store dir.
func (s *Server) watch() {
	wt, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	addDirs(wt, s.eng.S.NotesDir())
	_ = wt.Add(s.eng.S.Dir)
	go func() {
		defer func() { _ = wt.Close() }()
		for {
			select {
			case ev, ok := <-wt.Events:
				if !ok {
					return
				}
				if ev.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = wt.Add(ev.Name) // watch newly-created subfolders
					}
				}
				s.hub.broadcast()
			case _, ok := <-wt.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

func addDirs(wt *fsnotify.Watcher, root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			_ = wt.Add(p)
		}
		return nil
	})
}

// ---- tree + helpers -------------------------------------------------------

type treeNode struct {
	Name     string
	URL      string
	IsNote   bool
	Current  bool
	Children []*treeNode
}

// buildTree groups notes into a nested folder tree from their Rel paths.
// activeHandle is the handle of the note currently being viewed ("" on index).
func buildTree(notes []*note.Note, activeHandle string) []*treeNode {
	root := &treeNode{}
	dirs := map[string]*treeNode{"": root}
	for _, n := range notes {
		folder, _ := splitRel(n.Rel)
		h := noteHandle(n)
		parent := ensureDir(dirs, root, folder)
		parent.Children = append(parent.Children, &treeNode{
			Name:    n.Title,
			URL:     "/n/" + url.PathEscape(h),
			IsNote:  true,
			Current: activeHandle != "" && h == activeHandle,
		})
	}
	sortTree(root)
	return root.Children
}

func ensureDir(dirs map[string]*treeNode, root *treeNode, path string) *treeNode {
	if path == "" {
		return root
	}
	if n, ok := dirs[path]; ok {
		return n
	}
	parentPath, name := splitRel(path)
	parent := ensureDir(dirs, root, parentPath)
	node := &treeNode{Name: name}
	parent.Children = append(parent.Children, node)
	dirs[path] = node
	return node
}

func sortTree(n *treeNode) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.IsNote != b.IsNote {
			return !a.IsNote // folders before notes
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	for _, c := range n.Children {
		if !c.IsNote {
			sortTree(c)
		}
	}
}

// dateOnly trims an RFC3339 timestamp to its YYYY-MM-DD date for display.
func dateOnly(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// splitRel splits a slash path into (parent, last). "work/auth/x.md" → ("work/auth","x.md").
func splitRel(rel string) (parent, last string) {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i], rel[i+1:]
	}
	return "", rel
}

// noteHandle is a note's stable URL handle: its ULID when it has one, else its
// notes/-relative path. Notes authored outside nt (e.g. in Obsidian/an editor)
// have no id, so they're addressed by path — otherwise their URL would be the
// broken "/n/".
func noteHandle(n *note.Note) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Rel
}

// findByHandle matches a note by its id or its relative path.
func findByHandle(notes []*note.Note, h string) *note.Note {
	for _, n := range notes {
		if (n.ID != "" && n.ID == h) || n.Rel == h {
			return n
		}
	}
	return nil
}

func findByPath(notes []*note.Note, path string) *note.Note {
	for _, n := range notes {
		if n.Path == path {
			return n
		}
	}
	return nil
}
