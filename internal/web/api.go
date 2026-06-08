package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/web/apitypes"
)

// This file is the JSON API the Svelte/TypeScript SPA consumes — Phase 1 of the
// web rebuild (see docs/web-redesign-proposal.md). It is additive: the existing
// server-rendered (htmx) UI is untouched and remains the default. Every read
// projects from the in-memory snapshot (readmodel.go); every write still goes
// through mutate.Engine.Apply, so the SPA gets the same lock + re-read + undo
// safety as the CLI/agent. Note bodies are rendered server-side (goldmark) so
// wikilink resolution + Chroma highlighting stay identical across adapters.

func (s *Server) apiRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", s.apiState)
	mux.HandleFunc("GET /api/notes", s.apiNotes)
	mux.HandleFunc("GET /api/notes/{handle}", s.apiNote)
	mux.HandleFunc("GET /api/notes/{handle}/raw", s.apiNoteRaw)
	mux.HandleFunc("POST /api/notes/{handle}", s.apiNoteSave)
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
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

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
	})
}

func (s *Server) apiNotes(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	writeJSON(w, apitypes.NotesIndex{Tree: toTree(buildTree(snap.notes, "")), Index: toLinks(snap.index)})
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
	writeJSON(w, buildGraphData(s.current()))
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

// apiSearch mirrors handleSearch's resolution (title match + ripgrep literal,
// optional tag filter) but returns JSON. Ranked snippets are a planned upgrade.
func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	snap := s.current()
	byPath := make(map[string]*note.Note, len(snap.notes))
	for _, n := range snap.notes {
		byPath[n.Path] = n
	}
	seen := map[string]bool{}
	results := make([]apitypes.NoteLink, 0)
	add := func(n *note.Note) {
		if n == nil || seen[noteHandle(n)] || (tag != "" && !contains(n.Tags, tag)) {
			return
		}
		seen[noteHandle(n)] = true
		results = append(results, apitypes.NoteLink{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
	}
	switch {
	case q == "" && tag == "":
		// nothing
	case q == "":
		for _, n := range snap.notes {
			add(n)
		}
	default:
		ql := strings.ToLower(q)
		for _, n := range snap.notes {
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
	writeJSON(w, apitypes.SearchResponse{Results: results})
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
		t := task.New(txt)
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
