// Package web is the HTTP adapter: a localhost notes viewer (`nt web`). It is a
// read adapter over the same domain (note/links/mutate) the CLI, TUI, and MCP
// server use — so it shows exactly what they store. It is deliberately
// structured to make editing a future additive change: state lives on the
// Server struct, notes are addressed by stable id, and the render path is one
// reusable function.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
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
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

//go:embed assets
var assetsFS embed.FS

// Server holds the shared state for the viewer. (Editing, when added, hangs
// write routes and a CSRF token here — seam #1.)
type Server struct {
	eng       *mutate.Engine
	version   string
	tmpl      *template.Template
	hub       *hub
	hlCSS     string // generated Chroma syntax-highlight stylesheet (theme-scoped)
	allowEdit bool   // writes enabled (nt web --edit); read-only by default
	csrf      string // per-process token required on save (blocks cross-site POSTs)
}

// NewServer parses the embedded template and prepares the viewer.
func NewServer(eng *mutate.Engine, version string) (*Server, error) {
	funcs := template.FuncMap{
		// json marshals a value for safe embedding in a <script> tag (Go's
		// encoder escapes <, >, & so a note title can't break out).
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b) //nolint:gosec // HTML-escaped JSON, data only
		},
	}
	tmpl, err := template.New("nt").Funcs(funcs).ParseFS(assetsFS, "assets/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{eng: eng, version: version, tmpl: tmpl, hub: newHub(), hlCSS: highlightCSS(), csrf: randToken()}, nil
}

func randToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "nt-static-token" // dev fallback; localhost only
	}
	return hex.EncodeToString(b)
}

// Serve opens the store and serves the viewer on addr (e.g. "127.0.0.1:0").
// allowEdit enables note editing in the browser (read-only when false).
func Serve(version, addr string, allowEdit bool) error {
	eng, err := mutate.Open()
	if err != nil {
		return err
	}
	s, err := NewServer(eng, version)
	if err != nil {
		return err
	}
	s.allowEdit = allowEdit
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.watch()
	fmt.Printf("nt web — serving notes at http://%s\n", ln.Addr().String())
	mode := "read-only"
	if allowEdit {
		mode = "editing enabled (--edit)"
	}
	fmt.Printf("(localhost only · %s · live-reloads on change · Ctrl+C to stop)\n", mode)
	return http.Serve(ln, s.routes()) //nolint:gosec // localhost dev server, no timeouts needed
}

// Handler returns the viewer's HTTP handler so another host can serve the UI
// without nt binding a TCP port — e.g. a Wails desktop shell wiring it into
// assetserver.Options.Handler. This is the seam that lets the exact same
// server-rendered UI run as a native app. Call SetEdit/StartWatch first if you
// want editing or live-reload.
func (s *Server) Handler() http.Handler { return s.routes() }

// SetEdit toggles in-app editing (CSRF-guarded). Read-only by default.
func (s *Server) SetEdit(v bool) { s.allowEdit = v }

// StartWatch begins watching the store and pushing SSE live-reload events.
// Serve calls this itself; embedders call it when they want live-reload.
func (s *Server) StartWatch() { s.watch() }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/n/", s.handleNote)
	mux.HandleFunc("/search", s.handleSearch)
	mux.HandleFunc("/orphans", s.handleOrphans)
	mux.HandleFunc("/tags", s.handleTags)
	mux.HandleFunc("/tasks", s.handleTasks)
	mux.HandleFunc("/graph", s.handleGraph)
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
	NoteIndex   []linkRow // flat list of every note, for the ⌘K palette
	ShowResults bool
	SearchQuery string
	SearchTag   string
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
	Prev        *linkRow
	Next        *linkRow

	IsState    bool
	StateTitle string
	Target     string
	Candidates []linkRow

	Empty      bool // store has no notes (onboarding state)
	IsTags     bool
	TagRows    []tagRow
	IsTasks    bool
	TaskGroups []taskGroup
	IsGraph    bool
	GraphSrc   string // mermaid source for the link graph

	CanEdit bool   // editing enabled (nt web --edit)
	CSRF    string // token the editor sends back on save
}

type linkRow struct{ URL, Title, Path string }
type tagRow struct {
	Name  string
	Count int
	URL   string
}
type taskGroup struct {
	Status string
	Tasks  []taskRow
}
type taskRow struct {
	Text, Status, Due, Source, Project string
	Tags                               []string
}

// flatNotes is the note index the ⌘K palette filters over.
func flatNotes(notes []*note.Note) []linkRow {
	out := make([]linkRow, 0, len(notes))
	for _, n := range notes {
		out = append(out, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
	}
	return out
}

func (s *Server) render(w http.ResponseWriter, notes []*note.Note, d *pageData) {
	d.NoteIndex = flatNotes(notes)
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
	s.render(w, notes, &pageData{Title: "nt notes", Tree: buildTree(notes, ""), Empty: len(notes) == 0})
}

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	handle, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/n/"))
	if handle == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost { // save (editing)
		s.handleSave(w, r, handle)
		return
	}
	doc, notes := s.load()
	if r.URL.Query().Get("missing") != "1" {
		if n := findByHandle(notes, handle); n != nil {
			if r.URL.Query().Get("preview") == "1" { // hover preview
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"title": n.Title, "snippet": previewSnippet(n.Body)})
				return
			}
			if r.URL.Query().Get("raw") == "1" { // raw source for the editor
				if !s.allowEdit {
					http.NotFound(w, r)
					return
				}
				if data, err := store.ReadFile(n.Path); err == nil {
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					_, _ = w.Write(data)
				}
				return
			}
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
	prev, next := siblings(notes, n, folder)
	s.render(w, notes, &pageData{
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
		Prev:        prev,
		Next:        next,
		CanEdit:     s.allowEdit,
		CSRF:        s.csrf,
	})
}

// handleSave writes an edited note back to disk (nt web --edit). It is guarded by
// a per-process CSRF token (sent as a custom header, which forces a CORS preflight
// that a cross-site page can't satisfy) — and editing is off unless --edit.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request, handle string) {
	if !s.allowEdit {
		http.Error(w, "editing is disabled — start with `nt web --edit`", http.StatusForbidden)
		return
	}
	if r.Header.Get("X-CSRF") != s.csrf {
		http.Error(w, "bad or missing CSRF token", http.StatusForbidden)
		return
	}
	_, notes := s.load()
	n := findByHandle(notes, handle)
	if n == nil {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > 0 && body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	if err := store.WriteAtomic(n.Path, body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// siblings returns the previous/next notes in the same folder (Rel order).
func siblings(notes []*note.Note, n *note.Note, folder string) (prev, next *linkRow) {
	var sibs []*note.Note
	for _, x := range notes {
		if f, _ := splitRel(x.Rel); f == folder {
			sibs = append(sibs, x) // notes arrive sorted by Rel
		}
	}
	for i, x := range sibs {
		if x.Path != n.Path {
			continue
		}
		row := func(m *note.Note) *linkRow {
			return &linkRow{URL: "/n/" + url.PathEscape(noteHandle(m)), Title: m.Title}
		}
		if i > 0 {
			prev = row(sibs[i-1])
		}
		if i < len(sibs)-1 {
			next = row(sibs[i+1])
		}
		break
	}
	return prev, next
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
	s.render(w, notes, &pageData{
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
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	_, notes := s.load()
	if q == "" && tag == "" {
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
		if n == nil || seen[noteHandle(n)] || (tag != "" && !contains(n.Tags, tag)) {
			return
		}
		seen[noteHandle(n)] = true
		results = append(results, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
	}
	if q == "" { // tag-only: list every note carrying the tag
		for _, n := range notes {
			add(n)
		}
	} else {
		ql := strings.ToLower(q)
		for _, n := range notes { // title matches first (most relevant)
			if strings.Contains(strings.ToLower(n.Title), ql) {
				add(n)
			}
		}
		if hits, err := search.Literal(q, s.eng.S.NotesDir()); err == nil {
			for _, h := range hits {
				add(byPath[h.Path])
			}
		}
	}
	if r.URL.Query().Get("json") == "1" { // search-as-you-type
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
		return
	}
	label := "Search: " + q
	if q == "" {
		label = "Tag: " + tag
	}
	s.render(w, notes, &pageData{
		Title:       label,
		Tree:        buildTree(notes, ""),
		ShowResults: true,
		SearchQuery: q,
		SearchTag:   tag,
		Results:     results,
	})
}

// handleOrphans lists notes with no inbound links — navigation gaps to curate.
func (s *Server) handleOrphans(w http.ResponseWriter, r *http.Request) {
	_, notes := s.load()
	var results []linkRow
	for _, n := range notes {
		if len(links.Backlinks(s.eng.S, n.ID, n.Rel)) == 0 {
			results = append(results, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
		}
	}
	s.render(w, notes, &pageData{Title: "Orphans", Tree: buildTree(notes, ""), ShowResults: true, Results: results})
}

// handleTags lists the tag vocabulary with counts; each links to its filtered set.
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	doc, notes := s.load()
	counts := map[string]int{}
	for _, n := range notes {
		for _, t := range n.Tags {
			counts[t]++
		}
	}
	if doc != nil {
		for _, tk := range doc.Tasks() {
			for _, t := range tk.Tags() {
				counts[t]++
			}
		}
	}
	names := make([]string, 0, len(counts))
	for k := range counts {
		names = append(names, k)
	}
	sort.Strings(names)
	rows := make([]tagRow, 0, len(names))
	for _, k := range names {
		rows = append(rows, tagRow{Name: k, Count: counts[k], URL: "/search?tag=" + url.QueryEscape(k)})
	}
	s.render(w, notes, &pageData{Title: "Tags", Tree: buildTree(notes, ""), IsTags: true, TagRows: rows})
}

// handleTasks is the agent-memory dashboard: tasks grouped by status, urgency-sorted.
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	doc, notes := s.load()
	var groups []taskGroup
	if doc != nil {
		tasks := doc.Tasks()
		task.SortByUrgency(tasks)
		byStatus := map[string][]taskRow{}
		for _, t := range tasks {
			row := taskRow{Text: cleanTaskText(t.Text), Status: t.Status(), Due: t.Due(), Source: t.Source(), Tags: t.Tags()}
			if p := t.Projects(); len(p) > 0 {
				row.Project = p[0]
			}
			byStatus[t.Status()] = append(byStatus[t.Status()], row)
		}
		for _, st := range []string{"doing", "open", "blocked", "done"} {
			if rows := byStatus[st]; len(rows) > 0 {
				groups = append(groups, taskGroup{Status: st, Tasks: rows})
			}
		}
	}
	s.render(w, notes, &pageData{Title: "Tasks", Tree: buildTree(notes, ""), IsTasks: true, TaskGroups: groups})
}

// handleGraph renders the note link graph as a clickable Mermaid diagram (reuses
// the vendored mermaid; the source is built from wikilink adjacency).
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	_, notes := s.load()
	s.render(w, notes, &pageData{Title: "Graph", Tree: buildTree(notes, ""), IsGraph: true, GraphSrc: graphSource(notes)})
}

func graphSource(notes []*note.Note) string {
	if len(notes) == 0 {
		return ""
	}
	idOf := make(map[string]string, len(notes))
	var b strings.Builder
	b.WriteString("graph LR\n")
	for i, n := range notes {
		nid := fmt.Sprintf("n%d", i)
		idOf[n.Path] = nid
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", nid, graphLabel(n.Title))
	}
	seen := map[string]bool{}
	for _, n := range notes {
		from := idOf[n.Path]
		for _, raw := range links.Wikilinks(n.Body) {
			if it, ok := links.Resolve(raw, nil, notes); ok && it.Kind == "note" {
				to := idOf[it.Path]
				if to == "" || to == from || seen[from+">"+to] {
					continue
				}
				seen[from+">"+to] = true
				fmt.Fprintf(&b, "  %s --> %s\n", from, to)
			}
		}
	}
	for i, n := range notes {
		fmt.Fprintf(&b, "  click n%d \"/n/%s\"\n", i, url.PathEscape(noteHandle(n)))
	}
	return b.String()
}

func graphLabel(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\"", "'"), "\n", " ")
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	return s
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/static/") {
	case "style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		s.writeAsset(w, "assets/style.css")
	case "highlight.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = io.WriteString(w, s.hlCSS)
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
	Path     string // folder path (persistence key for collapse state); "" for notes
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
	node := &treeNode{Name: name, Path: path}
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

var mdNoise = strings.NewReplacer("[[", "", "]]", "", "#", "", "*", "", "`", "", ">", "")

// previewSnippet builds a short plain-text excerpt of a note body for the hover
// popover: drops a leading H1, strips light Markdown noise, collapses whitespace.
func previewSnippet(body string) string {
	s := strings.TrimSpace(body)
	if strings.HasPrefix(s, "# ") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	s = strings.Join(strings.Fields(mdNoise.Replace(s)), " ")
	if len(s) > 220 {
		s = s[:220] + "…"
	}
	return s
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
