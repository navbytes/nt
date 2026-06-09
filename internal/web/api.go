package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/quickadd"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/web/apitypes"
)

// This file is the JSON API the Svelte/TypeScript SPA consumes — Phase 1 of the
// web rebuild. It is additive: the existing
// server-rendered (htmx) UI is untouched and remains the default. Every read
// projects from the in-memory snapshot (readmodel.go); every write still goes
// through mutate.Engine.Apply, so the SPA gets the same lock + re-read + undo
// safety as the CLI/agent. Note bodies are rendered server-side (goldmark) so
// wikilink resolution + Chroma highlighting stay identical across adapters.

func (s *Server) apiRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", s.apiState)
	mux.HandleFunc("GET /api/notes", s.apiNotes)
	mux.HandleFunc("GET /api/notes/grid", s.apiNotesGrid)
	mux.HandleFunc("POST /api/notes", s.apiNoteCreate)
	mux.HandleFunc("GET /api/notes/{handle}", s.apiNote)
	mux.HandleFunc("GET /api/notes/{handle}/raw", s.apiNoteRaw)
	mux.HandleFunc("POST /api/notes/{handle}", s.apiNoteSave)
	mux.HandleFunc("POST /api/notes/{handle}/move", s.apiNoteMove)
	mux.HandleFunc("POST /api/preview", s.handlePreview) // returns rendered HTML
	mux.HandleFunc("GET /api/tasks", s.apiTasks)
	mux.HandleFunc("POST /api/tasks", s.apiTaskNew)
	mux.HandleFunc("POST /api/tasks/{id}/done", s.apiTaskDone)
	mux.HandleFunc("POST /api/tasks/{id}/reopen", s.apiTaskReopen)
	mux.HandleFunc("POST /api/tasks/{id}/status", s.apiTaskStatus)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.apiTaskDelete)
	mux.HandleFunc("GET /api/activity", s.apiActivity)
	mux.HandleFunc("GET /api/search", s.apiSearch)
	mux.HandleFunc("GET /api/tags", s.apiTags)
	mux.HandleFunc("GET /api/orphans", s.apiOrphans)
	mux.HandleFunc("GET /api/graph", s.apiGraph)
	mux.HandleFunc("GET /api/journal", s.apiJournal)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// safeNoteFolder allowlists a note subfolder: letters, numbers, spaces, and
// '/'/'-'/'_'. It forbids '.' (so no "..") and '\\', so a folder from the web
// can't be coaxed into a path-traversal — the boundary guard for go/path-injection.
var safeNoteFolder = regexp.MustCompile(`^[\p{L}\p{N} /_-]+$`)

// maxSearchResults bounds the /api/search payload (E4); the client is told when
// more matched so it can prompt the user to narrow the query.
const maxSearchResults = 50

// ---- projections to the wire contract (apitypes) ---------------------------
// The wire structs live in the apitypes package (the single source tygo turns
// into TS); these converters project the web package's internal read-model
// structs onto them, so v1's server-rendered types stay independent.

func toTask(t taskRow) apitypes.Task {
	return apitypes.Task{
		ID: t.ID, Text: t.Text, Status: t.Status, Due: t.Due,
		Source: t.Source, Project: t.Project, Tags: t.Tags, Blocker: t.Blocker,
	}
}

func toGroups(gs []taskGroup) []apitypes.TaskGroup {
	out := make([]apitypes.TaskGroup, len(gs))
	for i, g := range gs {
		tasks := make([]apitypes.Task, len(g.Tasks))
		for j, t := range g.Tasks {
			tasks[j] = toTask(t)
		}
		out[i] = apitypes.TaskGroup{Status: g.Status, Tasks: tasks}
	}
	return out
}

func toTree(ns []*treeNode) []apitypes.TreeNode {
	out := make([]apitypes.TreeNode, len(ns))
	for i, n := range ns {
		out[i] = apitypes.TreeNode{
			Name: n.Name, Path: n.Path, URL: n.URL, IsNote: n.IsNote,
			Children: toTree(n.Children),
		}
	}
	return out
}

func toLink(l linkRow) apitypes.NoteLink {
	return apitypes.NoteLink{URL: l.URL, Title: l.Title, Path: l.Path}
}

func toLinks(ls []linkRow) []apitypes.NoteLink {
	out := make([]apitypes.NoteLink, len(ls))
	for i, l := range ls {
		out[i] = toLink(l)
	}
	return out
}

func toLinkPtr(l *linkRow) *apitypes.NoteLink {
	if l == nil {
		return nil
	}
	v := toLink(*l)
	return &v
}

func toBacklinks(bs []Backlink) []apitypes.Backlink {
	out := make([]apitypes.Backlink, len(bs))
	for i, b := range bs {
		out[i] = apitypes.Backlink{Title: b.Title, URL: b.URL, Text: b.Text, IsNote: b.IsNote}
	}
	return out
}

func toTaskRefs(rs []TaskRef) []apitypes.TaskRef {
	out := make([]apitypes.TaskRef, len(rs))
	for i, r := range rs {
		out[i] = apitypes.TaskRef{Text: r.Text, Status: r.Status, Source: r.Source}
	}
	return out
}

func toActivity(days []activityDay) []apitypes.ActivityDay {
	out := make([]apitypes.ActivityDay, len(days))
	for i, d := range days {
		evs := make([]apitypes.ActivityEvent, len(d.Events))
		for j, e := range d.Events {
			evs[j] = apitypes.ActivityEvent{
				When: e.When.Format(time.RFC3339), Action: e.Action, Kind: e.Kind,
				Source: e.Source, Title: e.Title, URL: e.URL,
			}
		}
		out[i] = apitypes.ActivityDay{Date: d.Date, Events: evs}
	}
	return out
}

// ---- read handlers ---------------------------------------------------------

func (s *Server) apiState(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	open := 0
	if snap.doc != nil {
		for _, t := range snap.doc.Tasks() {
			if !t.Done {
				open++
			}
		}
	}
	csrf := ""
	if s.allowEdit {
		csrf = s.csrf
	}
	writeJSON(w, apitypes.State{
		CanEdit:   s.allowEdit,
		CSRF:      csrf,
		Version:   s.version,
		OpenCount: open,
		NoteCount: len(snap.notes),
		Sources:   snap.sources,
		Warning:   snap.readErr,
	})
}

func (s *Server) apiNotes(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	writeJSON(w, apitypes.NotesIndex{Tree: toTree(buildTree(snap.notes, "")), Index: toLinks(snap.index)})
}

// apiNotesGrid projects every note as a card (title/folder/tags/preview/updated)
// for the /notes overview, plus the distinct folders for the filter.
func (s *Server) apiNotesGrid(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	folders := map[string]bool{}
	cards := make([]apitypes.NoteCard, 0, len(snap.notes))
	for _, n := range snap.notes {
		folder, _ := splitRel(n.Rel)
		if folder != "" {
			folders[folder] = true
		}
		updated := n.Updated
		if updated == "" {
			updated = n.Created
		}
		cards = append(cards, apitypes.NoteCard{
			Handle:  noteHandle(n),
			Title:   n.Title,
			URL:     "/n/" + url.PathEscape(noteHandle(n)),
			Folder:  folder,
			Tags:    n.Tags,
			Preview: notePreview(n.Body),
			Updated: dateOnly(updated),
		})
	}
	folderList := make([]string, 0, len(folders))
	for f := range folders {
		folderList = append(folderList, f)
	}
	sort.Strings(folderList)
	writeJSON(w, apitypes.NotesGrid{Notes: cards, Folders: folderList})
}

// notePreview returns a short plain-text snippet of a note body for a card: it
// drops a leading "# H1" (usually the title, already shown), strips simple
// markdown markers, collapses whitespace, and caps the length.
func notePreview(body string) string {
	const max = 180
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue // skip blanks and headings (the H1 is the title)
		}
		l = strings.TrimLeft(l, "->*+ \t") // list/quote markers
		if l == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(l)
		if b.Len() >= max {
			break
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if len(s) > max {
		s = strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

func (s *Server) apiNote(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	handle := r.PathValue("handle")
	n := snap.findHandle(handle)
	if n == nil {
		http.NotFound(w, r)
		return
	}
	html, err := renderBody(n.Body, snap.doc, snap.notes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	folder, file := splitRel(n.Rel)
	var crumbs []string
	if folder != "" {
		crumbs = strings.Split(folder, "/")
	}
	prev, next := siblings(snap.notes, n, folder)
	raw, _ := store.ReadFile(n.Path)
	writeJSON(w, apitypes.NoteView{
		ID:        noteHandle(n),
		Title:     n.Title,
		Folder:    folder,
		File:      file,
		Crumbs:    crumbs,
		Source:    n.Source,
		Created:   dateOnly(n.Created),
		Tags:      n.Tags,
		BodyHTML:  string(html),
		Backlinks: toBacklinks(snap.backlinks[n.Path]),
		TaskRefs:  toTaskRefs(snap.taskRefs[n.Path]),
		Prev:      toLinkPtr(prev),
		Next:      toLinkPtr(next),
		ETag:      etag(raw),
	})
}

func (s *Server) apiNoteRaw(w http.ResponseWriter, r *http.Request) {
	if !s.allowEdit {
		http.NotFound(w, r)
		return
	}
	n := s.current().findHandle(r.PathValue("handle"))
	if n == nil {
		http.NotFound(w, r)
		return
	}
	data, err := store.ReadFile(n.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, apitypes.RawNote{Text: string(data), ETag: etag(data)})
}

// apiNoteSave reuses the existing save path (--edit + CSRF + If-Match → 409 +
// atomic write + rebuild) — the lost-update guard is preserved verbatim.
func (s *Server) apiNoteSave(w http.ResponseWriter, r *http.Request) {
	s.handleSave(w, r, r.PathValue("handle"))
}

// apiNoteMove moves a note into a different folder (--edit + CSRF gated),
// rewriting every [[link]] to it via the shared RenameNote. The note keeps its
// filename and its id, so its handle/URL is unchanged. An empty folder moves it
// to the notes/ root.
func (s *Server) apiNoteMove(w http.ResponseWriter, r *http.Request) {
	if !s.allowEdit {
		http.Error(w, "editing is disabled — start with `nt web --edit`", http.StatusForbidden)
		return
	}
	if r.Header.Get("X-CSRF") != s.csrf {
		http.Error(w, "bad or missing CSRF token", http.StatusForbidden)
		return
	}
	snap := s.current()
	n := snap.findHandle(r.PathValue("handle"))
	if n == nil {
		http.NotFound(w, r)
		return
	}
	folder := strings.Trim(strings.TrimSpace(r.FormValue("folder")), "/")
	if folder != "" && !safeNoteFolder.MatchString(folder) {
		http.Error(w, "folder may contain only letters, numbers, spaces, '/', '-', '_'", http.StatusBadRequest)
		return
	}
	dest := path.Base(n.Rel)
	if folder != "" {
		dest = folder + "/" + dest
	}
	newRel, updated, err := s.eng.RenameNote(n, snap.notes, dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writes.mark(filepath.Join(s.eng.S.NotesDir(), filepath.FromSlash(newRel)))
	s.rebuild()
	s.hub.broadcast("reload")
	writeJSON(w, apitypes.MovedNote{
		Handle: noteHandle(n), URL: "/n/" + url.PathEscape(noteHandle(n)), Rel: newRel, Updated: updated,
	})
}

// apiNoteCreate creates a new note (--edit + CSRF gated) and returns its handle
// + URL. A "folder/Title" value in the title is split into a subfolder, mirroring
// the `nt note` CLI shorthand.
func (s *Server) apiNoteCreate(w http.ResponseWriter, r *http.Request) {
	if !s.allowEdit {
		http.Error(w, "editing is disabled — start with `nt web --edit`", http.StatusForbidden)
		return
	}
	if r.Header.Get("X-CSRF") != s.csrf {
		http.Error(w, "bad or missing CSRF token", http.StatusForbidden)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	folder := strings.TrimSpace(r.FormValue("folder"))
	if folder == "" { // "work/Auth design" → folder "work", title "Auth design"
		if i := strings.LastIndex(title, "/"); i >= 0 {
			folder = strings.TrimSpace(title[:i])
			title = strings.TrimSpace(title[i+1:])
		}
	}
	if title == "" {
		http.Error(w, "a note title is required", http.StatusBadRequest)
		return
	}
	// Allowlist the folder at the boundary: it becomes a real directory path, so
	// forbid anything but letters/numbers/space/'/'/'-'/'_' — no "." (hence no
	// "..") and no "\\". The title is safe regardless (note.Slug strips it to
	// [a-z0-9-] for the filename).
	if folder != "" && !safeNoteFolder.MatchString(folder) {
		http.Error(w, "folder may contain only letters, numbers, spaces, '/', '-', '_'", http.StatusBadRequest)
		return
	}
	n, err := note.Create(s.eng.S, title, "", nil, "web", folder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writes.mark(n.Path) // suppress our own fsnotify event
	s.rebuild()
	s.hub.broadcast("reload") // other tabs pick up the new note
	writeJSON(w, apitypes.CreatedNote{Handle: noteHandle(n), URL: "/n/" + url.PathEscape(noteHandle(n))})
}

func (s *Server) apiTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, apitypes.TasksResponse{Groups: toGroups(buildTaskGroups(s.current().doc))})
}

func (s *Server) apiActivity(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	writeJSON(w, apitypes.ActivityResponse{
		Days:    toActivity(groupActivity(snap.activity, r.URL.Query().Get("source"))),
		Sources: snap.sources,
	})
}

func (s *Server) apiGraph(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, toGraph(buildGraphData(s.current())))
}

func toGraph(g *graphData) apitypes.GraphData {
	nodes := make([]apitypes.GraphNode, len(g.Nodes))
	for i, n := range g.Nodes {
		nodes[i] = apitypes.GraphNode{
			ID: n.ID, Kind: n.Kind, Title: n.Title, URL: n.URL, Folder: n.Folder,
			Source: n.Source, Tags: n.Tags, Deg: n.Deg,
		}
	}
	links := make([]apitypes.GraphLink, len(g.Links))
	for i, l := range g.Links {
		links[i] = apitypes.GraphLink{S: l.S, T: l.T}
	}
	return apitypes.GraphData{Nodes: nodes, Links: links, Truncated: g.Truncated}
}

// apiTags lists the tag vocabulary (note + task tags) with counts, sorted by
// name — same projection as the v1 /tags page.
func (s *Server) apiTags(w http.ResponseWriter, r *http.Request) {
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
	tags := make([]apitypes.Tag, 0, len(names))
	for _, k := range names {
		tags = append(tags, apitypes.Tag{Name: k, Count: counts[k]})
	}
	writeJSON(w, apitypes.TagsResponse{Tags: tags})
}

// apiOrphans lists notes that participate in no links (none in, none out).
func (s *Server) apiOrphans(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	notes := make([]apitypes.NoteLink, 0)
	for _, n := range snap.notes {
		if !snap.linked[n.Path] {
			notes = append(notes, apitypes.NoteLink{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
		}
	}
	writeJSON(w, apitypes.OrphansResponse{Notes: notes})
}

// apiSearch ranks title matches first, then body matches with snippets, with an
// optional tag filter. It scans the in-memory read-model (no per-query ripgrep).
func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	snap := s.current()
	seen := map[string]bool{}
	results := make([]apitypes.SearchResult, 0)
	add := func(n *note.Note, snippet string) {
		if n == nil || seen[noteHandle(n)] || (tag != "" && !contains(n.Tags, tag)) {
			return
		}
		seen[noteHandle(n)] = true
		results = append(results, apitypes.SearchResult{
			URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel, Kind: "note", Snippet: snippet,
		})
	}
	// addTask appends a task hit (text + tag filter applied by the caller). Tasks
	// have no page of their own, so they all link to the task list (matching the
	// graph's task-node convention).
	addTask := func(t *task.Task, text string) {
		results = append(results, apitypes.SearchResult{
			URL: "/tasks", Title: text, Path: t.Source(), Kind: "task",
		})
	}
	taskTagged := func(t *task.Task) bool { return tag == "" || contains(t.Tags(), tag) }
	switch {
	case q == "" && tag == "":
		// nothing
	case q == "":
		for _, n := range snap.notes {
			add(n, "")
		}
		for _, t := range snap.doc.Tasks() {
			if taskTagged(t) {
				addTask(t, cleanTaskText(t.Text))
			}
		}
	default:
		// Rank title matches first (most relevant), then body matches — each
		// carrying the matching line as a snippet for context. Both scan the
		// in-memory read-model (note bodies are already parsed + cached), so a
		// search no longer spawns ripgrep per query and scales with the snapshot.
		// Tasks come last: their cleaned text is matched the same way.
		ql := strings.ToLower(q)
		for _, n := range snap.notes {
			if strings.Contains(strings.ToLower(n.Title), ql) {
				add(n, "")
			}
		}
		for _, n := range snap.notes {
			if line, ok := firstMatchingLine(n.Body, ql); ok {
				add(n, snippetAround(line, q))
			}
		}
		for _, t := range snap.doc.Tasks() {
			text := cleanTaskText(t.Text)
			if taskTagged(t) && strings.Contains(strings.ToLower(text), ql) {
				addTask(t, text)
			}
		}
	}
	// Bound the payload: return at most maxSearchResults, flagging when there
	// are more so the client can say so (E4).
	truncated := false
	if len(results) > maxSearchResults {
		results, truncated = results[:maxSearchResults], true
	}
	writeJSON(w, apitypes.SearchResponse{Results: results, Truncated: truncated})
}

// firstMatchingLine returns the first line of body containing the (already
// lower-cased) query, case-insensitively — the in-memory equivalent of a ripgrep
// hit, used to build a search snippet from the cached note body.
func firstMatchingLine(body, ql string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), ql) {
			return line, true
		}
	}
	return "", false
}

// snippetAround trims a matching line to a short window centered on the query,
// for display under a search result. The highlight is applied client-side.
func snippetAround(line, q string) string {
	line = strings.TrimSpace(line)
	const window = 160
	if len(line) <= window {
		return line
	}
	idx := strings.Index(strings.ToLower(line), strings.ToLower(q))
	if idx < 0 {
		return line[:window] + "…"
	}
	start := idx - window/3
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(line) {
		end = len(line)
		if start = end - window; start < 0 {
			start = 0
		}
	}
	out := line[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(line) {
		out += "…"
	}
	return out
}

// ---- task write handlers (JSON) --------------------------------------------

// respondTasks returns the refreshed task groups after a successful write.
func (s *Server) respondTasks(w http.ResponseWriter) {
	writeJSON(w, apitypes.TasksResponse{Groups: toGroups(buildTaskGroups(s.current().doc))})
}

func (s *Server) apiTaskDone(w http.ResponseWriter, r *http.Request) {
	if s.doTaskWrite(w, r, "done", func(d *task.Doc, rec *mutate.Recorder) error {
		t, err := resolveTask(d, r)
		if err != nil {
			return err
		}
		mutate.Complete(d, rec, t, mutate.Today())
		return nil
	}) {
		s.respondTasks(w)
	}
}

func (s *Server) apiTaskReopen(w http.ResponseWriter, r *http.Request) {
	if s.doTaskWrite(w, r, "reopen", func(d *task.Doc, rec *mutate.Recorder) error {
		t, err := resolveTask(d, r)
		if err != nil {
			return err
		}
		rec.Before(t)
		t.SetDone(false, "")
		t.SetState("open")
		return nil
	}) {
		s.respondTasks(w)
	}
}

func (s *Server) apiTaskStatus(w http.ResponseWriter, r *http.Request) {
	status := r.FormValue("status")
	if s.doTaskWrite(w, r, "update", func(d *task.Doc, rec *mutate.Recorder) error {
		t, err := resolveTask(d, r)
		if err != nil {
			return err
		}
		rec.Before(t)
		switch status {
		case "open":
			t.SetDone(false, "")
			t.SetState("open")
		case "doing", "blocked":
			t.SetState(status)
		case "done":
			mutate.Complete(d, rec, t, mutate.Today())
		default:
			return errBadStatus(status)
		}
		return nil
	}) {
		s.respondTasks(w)
	}
}

func (s *Server) apiTaskDelete(w http.ResponseWriter, r *http.Request) {
	if s.doTaskWrite(w, r, "delete", func(d *task.Doc, rec *mutate.Recorder) error {
		t, err := resolveTask(d, r)
		if err != nil {
			return err
		}
		before, ok := d.Remove(t.ID())
		if !ok {
			return errNoTask
		}
		rec.Removed(t.ID(), before)
		return nil
	}) {
		s.respondTasks(w)
	}
}

func (s *Server) apiTaskNew(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Error(w, "task text is required", http.StatusBadRequest)
		return
	}
	pri, due, project := r.FormValue("pri"), r.FormValue("due"), strings.TrimSpace(r.FormValue("project"))
	priByte, okP := byte(0), true
	if pri != "" {
		priByte, okP = dateparse.Priority(pri)
	}
	dueVal, okD := "", true
	if due != "" {
		dueVal, okD = dateparse.Date(due)
	}
	if !okP || !okD {
		http.Error(w, "invalid priority or due date", http.StatusBadRequest)
		return
	}
	if s.doTaskWrite(w, r, "add", func(d *task.Doc, rec *mutate.Recorder) error {
		txt := text
		if project != "" {
			txt += " +" + project
		}
		t := quickadd.New(txt) // normalize inline due:/t:/!pri the user typed
		if priByte != 0 {
			t.SetPriority(priByte)
		}
		if dueVal != "" {
			t.SetKey("due", dueVal)
		}
		t.SetKey("src", "web")
		d.Append(t)
		rec.Added(t)
		return nil
	}) {
		s.respondTasks(w)
	}
}

// small error helpers (kept out of the hot path)
func errBadStatus(s string) error { return &apiError{"invalid status " + s} }

var errNoTask = &apiError{"no such task"}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }
