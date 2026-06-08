// Package web is the HTTP adapter: a localhost notes viewer (`nt web`). It is a
// read adapter over the same domain (note/links/mutate) the CLI, TUI, and MCP
// server use — so it shows exactly what they store. It is deliberately
// structured to make editing a future additive change: state lives on the
// Server struct, notes are addressed by stable id, and the render path is one
// reusable function.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// Server holds the shared state for the viewer. (Editing, when added, hangs
// write routes and a CSRF token here — seam #1.)
type Server struct {
	eng       *mutate.Engine
	version   string
	hub       *hub
	hlCSS     string // generated Chroma syntax-highlight stylesheet (theme-scoped)
	hlETag    string // content-hash ETag for highlight.css (for 304s)
	allowEdit bool   // writes enabled (nt web --edit); read-only by default
	csrf      string // per-process token required on save (blocks cross-site POSTs)

	mu       sync.RWMutex  // guards snap + watching
	snap     *snapshot     // in-memory read-model (see readmodel.go)
	watching bool          // true once the fsnotify watcher maintains snap
	writes   *writeTracker // self-write suppression for the watcher
}

// NewServer initialises the server and warms the read-model.
func NewServer(eng *mutate.Engine, version string) (*Server, error) {
	css := highlightCSS()
	s := &Server{
		eng: eng, version: version, hub: newHub(),
		hlCSS: css, hlETag: etag([]byte(css)),
		csrf: randToken(), writes: newWriteTracker(),
	}
	s.snap = buildSnapshot(eng) // warm the read-model so the first request is fast
	return s, nil
}

// current returns the read-model. While the fsnotify watcher is running it
// serves the maintained snapshot (the fast path). When it is NOT — tests and
// embedders that never call StartWatch — it builds fresh per call, preserving
// the old read-through semantics so a write made directly to the store is seen
// immediately without a watcher.
func (s *Server) current() *snapshot {
	s.mu.RLock()
	if s.watching {
		snap := s.snap
		s.mu.RUnlock()
		return snap
	}
	s.mu.RUnlock()
	return buildSnapshot(s.eng)
}

// rebuild recomputes the read-model and swaps it in. Called by the watcher and
// synchronously by write handlers so the writer's next read is fresh.
func (s *Server) rebuild() {
	snap := buildSnapshot(s.eng)
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

// etag is a content validator for a note file's current bytes. The editor
// captures it on ?raw=1 load and returns it as If-Match on save so a concurrent
// write (agent, CLI, another tab) is detected rather than overwritten. Content-
// hashed (not mtime) so it's immune to filesystem timestamp granularity.
func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func randToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "nt-static-token" // dev fallback; localhost only
	}
	return hex.EncodeToString(b)
}

// Serve opens the store and serves the SPA on addr (e.g. "127.0.0.1:0").
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
	fmt.Printf("(localhost only · Svelte SPA · %s · live-reloads on change · Ctrl+C to stop)\n", mode)
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

// SetSPA is a no-op retained for embedder compatibility; the SPA is always served.
func (s *Server) SetSPA(_ bool) {}

// StartWatch begins watching the store and pushing SSE live-reload events.
// Serve calls this itself; embedders call it when they want live-reload.
func (s *Server) StartWatch() { s.watch() }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	s.apiRoutes(mux)
	mux.HandleFunc("/events", s.handleSSE)
	mux.HandleFunc("GET /static/highlight.css", s.handleHighlightCSS)
	_ = s.spaRoutes(mux) // embedded SPA bundle; always present in a built binary
	return mux
}

// load returns the current document and notes from the read-model.
func (s *Server) load() (*task.Doc, []*note.Note) {
	snap := s.current()
	return snap.doc, snap.notes
}

// ---- shared data types (used by both server.go helpers and api.go) --------

// activityDay groups timeline events under one calendar date.
type activityDay struct {
	Date   string          `json:"date"`
	Events []activityEvent `json:"events"`
}

type linkRow struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Path  string `json:"path"`
}
type taskGroup struct {
	Status string    `json:"status"`
	Tasks  []taskRow `json:"tasks"`
}
type taskRow struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Status  string   `json:"status"`
	Due     string   `json:"due,omitempty"`
	Source  string   `json:"source,omitempty"`
	Project string   `json:"project,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Blocker string   `json:"blocker,omitempty"`
}

// ---- handlers -------------------------------------------------------------

// handleHighlightCSS serves the Chroma syntax-highlight stylesheet with ETag
// caching. The SPA links to it from index.html so note bodies get correct
// syntax colours without embedding the CSS in every API response.
func (s *Server) handleHighlightCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", s.hlETag)
	if r.Header.Get("If-None-Match") == s.hlETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, s.hlCSS)
}

// groupActivity buckets events by calendar date (newest first), optionally
// keeping only one source.
func groupActivity(events []activityEvent, source string) []activityDay {
	var days []activityDay
	for _, e := range events {
		if source != "" && e.Source != source {
			continue
		}
		d := e.When.Format("Mon, Jan 2 2006")
		if n := len(days); n > 0 && days[n-1].Date == d {
			days[n-1].Events = append(days[n-1].Events, e)
		} else {
			days = append(days, activityDay{Date: d, Events: []activityEvent{e}})
		}
	}
	return days
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
	n := s.current().findHandle(handle)
	if n == nil {
		http.NotFound(w, r)
		return
	}
	// Lost-update guard: the editor captures the file's ETag on ?raw=1 and sends
	// it back as If-Match. If the bytes on disk no longer match (an agent, the
	// CLI, or another tab wrote this note since it was opened), refuse with 409
	// instead of silently clobbering that write. Absent If-Match (e.g. a non-JS
	// client) skips the check — the save is then plain last-writer-wins.
	if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
		cur, _ := store.ReadFile(n.Path)
		if etag(cur) != ifMatch {
			http.Error(w, "note changed on disk since you opened it — reload to merge", http.StatusConflict)
			return
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > 0 && body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	s.writes.mark(n.Path) // suppress our own fsnotify event (no self-reload)
	if err := store.WriteAtomic(n.Path, body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.rebuild() // refresh the read-model so the editor's reload sees the save
	w.WriteHeader(http.StatusNoContent)
}

// handlePreview renders an editor buffer to HTML for the live split-preview,
// reusing the exact renderBody path the note page uses (so wikilinks, mermaid,
// and syntax highlighting match what a save will produce). Edit-mode + CSRF
// gated; it reads but never writes.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if !s.allowEdit {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("X-CSRF") != s.csrf {
		http.Error(w, "bad or missing CSRF token", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snap := s.current()
	html, err := renderBody(stripFrontmatter(string(body)), snap.doc, snap.notes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, string(html))
}

// stripFrontmatter drops a leading YAML frontmatter block so the preview renders
// the body the way the note page does (note.List parses frontmatter out). The
// raw editor buffer still includes it; only the preview ignores it.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return s
	}
	rest := s[3:]
	if i := strings.Index(rest, "\n---"); i >= 0 {
		after := rest[i+4:]
		if j := strings.IndexByte(after, '\n'); j >= 0 {
			return after[j+1:]
		}
		return ""
	}
	return s
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


// buildTaskGroups groups tasks by status (urgency-sorted within each), in the
// display order doing → open → blocked → done.
func buildTaskGroups(doc *task.Doc) []taskGroup {
	if doc == nil {
		return nil
	}
	tasks := doc.Tasks()
	task.SortByUrgency(tasks)
	byStatus := map[string][]taskRow{}
	for _, t := range tasks {
		row := taskRow{ID: t.ID(), Text: cleanTaskText(t.Text), Status: t.Status(), Due: t.Due(), Source: t.Source(), Tags: t.Tags()}
		if p := t.Projects(); len(p) > 0 {
			row.Project = p[0]
		}
		byStatus[t.Status()] = append(byStatus[t.Status()], row)
	}
	var groups []taskGroup
	for _, st := range []string{"doing", "open", "blocked", "done"} {
		if rows := byStatus[st]; len(rows) > 0 {
			groups = append(groups, taskGroup{Status: st, Tasks: rows})
		}
	}
	return groups
}


// doTaskWrite enforces the --edit + CSRF gate, marks tasks.txt as a self-write
// (so the watcher doesn't also broadcast a full reload), runs fn through
// mutate.Engine.Apply (lock + re-read + undo journal), refreshes the read-model,
// and nudges other clients ("tasks"). Returns true on success; on failure it has
// already written the error response. Shared by the htmx and JSON task handlers.
func (s *Server) doTaskWrite(w http.ResponseWriter, r *http.Request, op string, fn func(d *task.Doc, rec *mutate.Recorder) error) bool {
	if !s.allowEdit {
		http.Error(w, "editing is disabled — start with `nt web --edit`", http.StatusForbidden)
		return false
	}
	if r.Header.Get("X-CSRF") != s.csrf {
		http.Error(w, "bad or missing CSRF token", http.StatusForbidden)
		return false
	}
	s.writes.mark(s.eng.S.TasksFile())
	if err := s.eng.Apply(op, fn); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	s.rebuild()
	s.hub.broadcast("tasks")
	return true
}

// resolveTask looks up the task addressed by the {id} path segment.
func resolveTask(d *task.Doc, r *http.Request) (*task.Task, error) {
	id := r.PathValue("id")
	if t, amb := d.Resolve(id); t != nil && !amb {
		return t, nil
	}
	return nil, fmt.Errorf("no such task %q", id)
}


// graphNode/graphLink/graphData are the JSON model the client force-directed
// canvas renders. Nodes carry folder/source/tags so the view can color and
// filter; links index into Nodes.
type graphNode struct {
	ID     string   `json:"id"`     // note handle (for /n/<id>)
	Title  string   `json:"title"`  //
	URL    string   `json:"url"`    //
	Folder string   `json:"folder"` // top-level folder (color/filter grouping)
	Source string   `json:"source"` // provenance (claude/cli/web/…)
	Tags   []string `json:"tags"`   //
	Deg    int      `json:"deg"`    // degree, for node sizing
}
type graphLink struct {
	S int `json:"s"`
	T int `json:"t"`
}
type graphData struct {
	Nodes []graphNode `json:"nodes"`
	Links []graphLink `json:"links"`
}

func buildGraphData(snap *snapshot) *graphData {
	idx := make(map[string]int, len(snap.notes))
	g := &graphData{Nodes: make([]graphNode, 0, len(snap.notes))}
	for i, n := range snap.notes {
		idx[n.Path] = i
		folder, _ := splitRel(n.Rel)
		if j := strings.IndexByte(folder, '/'); j >= 0 {
			folder = folder[:j] // top-level folder only
		}
		g.Nodes = append(g.Nodes, graphNode{
			ID:     noteHandle(n),
			Title:  n.Title,
			URL:    "/n/" + url.PathEscape(noteHandle(n)),
			Folder: folder,
			Source: n.Source,
			Tags:   n.Tags,
		})
	}
	seen := map[[2]int]bool{} // undirected dedup
	for path, outs := range snap.fwd {
		from, ok := idx[path]
		if !ok {
			continue
		}
		for _, tgt := range outs {
			to, ok := idx[tgt]
			if !ok || to == from {
				continue
			}
			key := [2]int{from, to}
			if from > to {
				key = [2]int{to, from}
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			g.Links = append(g.Links, graphLink{S: from, T: to})
			g.Nodes[from].Deg++
			g.Nodes[to].Deg++
		}
	}
	return g
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}


// ---- live reload (SSE + fsnotify) -----------------------------------------

type hub struct {
	mu      sync.Mutex
	clients map[chan string]bool
}

func newHub() *hub { return &hub{clients: map[chan string]bool{}} }

func (h *hub) add() chan string {
	ch := make(chan string, 4)
	h.mu.Lock()
	h.clients[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *hub) remove(ch chan string) {
	h.mu.Lock()
	if h.clients[ch] {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// broadcast pushes a typed event payload to every connected client. "reload"
// means "the page is stale, reload it" (an external change); finer kinds like
// "tasks" let a listener refresh just one fragment instead of the whole page.
func (h *hub) broadcast(kind string) {
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- kind:
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
	// Heartbeat: a periodic comment keeps the connection healthy and lets the
	// server notice (and reap) a client that vanished without a clean close, so
	// goroutines/sockets don't linger.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case kind, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", kind)
			fl.Flush()
		}
	}
}

// watchDebounce coalesces a burst of fsnotify events (an atomic rename fires
// several) into one rebuild (SPEC §6.5).
const watchDebounce = 80 * time.Millisecond

// watch maintains the read-model from disk and broadcasts live-reload. It is
// debounced and self-write-aware: events for files the adapter just wrote (and
// for hidden/temp files like the .nt-*.tmp atomic-write staging file) don't
// bounce clients, and a burst collapses into a single rebuild. Idempotent —
// safe to call more than once (Serve and embedders both may).
func (s *Server) watch() {
	s.mu.Lock()
	if s.watching {
		s.mu.Unlock()
		return
	}
	s.watching = true
	s.mu.Unlock()

	wt, err := fsnotify.NewWatcher()
	if err != nil {
		s.mu.Lock()
		s.watching = false
		s.mu.Unlock()
		return
	}
	addDirs(wt, s.eng.S.NotesDir())
	_ = wt.Add(s.eng.S.Dir)
	s.rebuild() // ensure the maintained snapshot is current before serving from it

	go func() {
		defer func() { _ = wt.Close() }()
		var (
			bmu      sync.Mutex
			timer    *time.Timer
			external bool // batch contains a change we didn't make ourselves
		)
		flush := func() {
			bmu.Lock()
			ext := external
			external = false
			bmu.Unlock()
			s.rebuild()
			if ext {
				s.hub.broadcast("reload") // only nudge clients for changes they didn't trigger
			}
		}
		for {
			select {
			case ev, ok := <-wt.Events:
				if !ok {
					return
				}
				if isTransient(ev.Name) {
					continue // .nt-*.tmp staging, lock/undo/log — not content the UI shows
				}
				if ev.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = wt.Add(ev.Name) // watch newly-created subfolders
					}
				}
				bmu.Lock()
				if !s.writes.isSelf(ev.Name) {
					external = true
				}
				if timer == nil {
					timer = time.AfterFunc(watchDebounce, flush)
				} else {
					timer.Reset(watchDebounce)
				}
				bmu.Unlock()
			case _, ok := <-wt.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

// isTransient reports whether a path is store bookkeeping the viewer never
// renders — the atomic-write staging file, the lock, the undo journal, the log,
// and dotfiles (.git/.obsidian). Changes to these must not trigger a rebuild or
// a live-reload; only tasks.txt / done.txt / notes matter.
func isTransient(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return true
	}
	switch base {
	case "tasks.txt.lock", "undo.jsonl", "nt.log":
		return true
	}
	return false
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
	Name     string      `json:"name"`
	Path     string      `json:"path"` // folder path (persistence key for collapse state); "" for notes
	URL      string      `json:"url"`
	IsNote   bool        `json:"isNote"`
	Current  bool        `json:"-"`
	Children []*treeNode `json:"children,omitempty"`
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
