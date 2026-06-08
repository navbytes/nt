package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
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
	mux.HandleFunc("GET /api/graph", s.apiGraph)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// ---- DTOs (the wire contract; tygo generates TS types from these) ----------

type apiStateDTO struct {
	CanEdit   bool     `json:"canEdit"`
	CSRF      string   `json:"csrf"` // token to echo as X-CSRF on writes; "" when read-only
	Version   string   `json:"version"`
	OpenCount int      `json:"openCount"`
	NoteCount int      `json:"noteCount"`
	Sources   []string `json:"sources"`
}

type apiNotesDTO struct {
	Tree  []*treeNode `json:"tree"`
	Index []linkRow   `json:"index"`
}

type apiNoteDTO struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Folder    string     `json:"folder"`
	File      string     `json:"file"`
	Crumbs    []string   `json:"crumbs"`
	Source    string     `json:"source"`
	Created   string     `json:"created"`
	Tags      []string   `json:"tags"`
	BodyHTML  string     `json:"bodyHTML"`
	Backlinks []Backlink `json:"backlinks"`
	TaskRefs  []TaskRef  `json:"taskRefs"`
	Prev      *linkRow   `json:"prev,omitempty"`
	Next      *linkRow   `json:"next,omitempty"`
	ETag      string     `json:"etag"`
}

type apiRawDTO struct {
	Text string `json:"text"`
	ETag string `json:"etag"`
}

type apiTasksDTO struct {
	Groups []taskGroup `json:"groups"`
}

type apiActivityDTO struct {
	Days    []activityDay `json:"days"`
	Sources []string      `json:"sources"`
}

type apiSearchDTO struct {
	Results []linkRow `json:"results"`
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
	writeJSON(w, apiStateDTO{
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
	writeJSON(w, apiNotesDTO{Tree: buildTree(snap.notes, ""), Index: snap.index})
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
	writeJSON(w, apiNoteDTO{
		ID:        noteHandle(n),
		Title:     n.Title,
		Folder:    folder,
		File:      file,
		Crumbs:    crumbs,
		Source:    n.Source,
		Created:   dateOnly(n.Created),
		Tags:      n.Tags,
		BodyHTML:  string(html),
		Backlinks: snap.backlinks[n.Path],
		TaskRefs:  snap.taskRefs[n.Path],
		Prev:      prev,
		Next:      next,
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
	writeJSON(w, apiRawDTO{Text: string(data), ETag: etag(data)})
}

// apiNoteSave reuses the existing save path (--edit + CSRF + If-Match → 409 +
// atomic write + rebuild) — the lost-update guard is preserved verbatim.
func (s *Server) apiNoteSave(w http.ResponseWriter, r *http.Request) {
	s.handleSave(w, r, r.PathValue("handle"))
}

func (s *Server) apiTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, apiTasksDTO{Groups: buildTaskGroups(s.current().doc)})
}

func (s *Server) apiActivity(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	writeJSON(w, apiActivityDTO{
		Days:    groupActivity(snap.activity, r.URL.Query().Get("source")),
		Sources: snap.sources,
	})
}

func (s *Server) apiGraph(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, buildGraphData(s.current()))
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
	var results []linkRow
	add := func(n *note.Note) {
		if n == nil || seen[noteHandle(n)] || (tag != "" && !contains(n.Tags, tag)) {
			return
		}
		seen[noteHandle(n)] = true
		results = append(results, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
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
	writeJSON(w, apiSearchDTO{Results: results})
}

// ---- task write handlers (JSON) --------------------------------------------

// respondTasks returns the refreshed task groups after a successful write.
func (s *Server) respondTasks(w http.ResponseWriter) {
	writeJSON(w, apiTasksDTO{Groups: buildTaskGroups(s.current().doc)})
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
