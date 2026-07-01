// Package mcp serves nt over the Model Context Protocol (stdio, JSON-RPC 2.0),
// so an agent uses typed tools (nt_ready, nt_add, …) instead of constructing CLI
// shell strings. It is a thin driving adapter: every tool reuses the same
// mutate.Engine + task/note domain as the CLI and TUI. No SDK dependency — the
// stdio transport is newline-delimited JSON-RPC, handled here directly.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/quickadd"
	"github.com/navbytes/nt/internal/recall"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/view"
	"github.com/navbytes/nt/internal/workstream"
)

const protocolVersion = "2024-11-05"

// Serve runs the MCP stdio loop against the global store until stdin closes.
func Serve(version string) error {
	e, err := mutate.Open()
	if err != nil {
		return err
	}
	return (&server{eng: e, version: version, cache: note.NewCache()}).loop(os.Stdin, os.Stdout)
}

type server struct {
	eng     *mutate.Engine
	version string
	cache   *note.Cache // parse cache for read handlers: re-stat every call, re-parse only what changed
}

// listNotes returns all notes via the per-server parse cache. It stats every note
// file each call (so reads stay fresh and read-your-writes holds) but re-parses
// only files that changed since the last call — turning the O(notes) re-parse that
// each read tool used to do into an O(changed) one. Falls back to an empty slice
// on error (retrieval prefers a partial answer over failing). Read handlers only:
// write handlers mutate notes and must not touch cache-shared pointers.
func (s *server) listNotes() []*note.Note {
	if s.cache == nil {
		ns, _ := note.List(s.eng.S)
		return ns
	}
	ns, _ := s.cache.List(s.eng.S)
	return ns
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *server) loop(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // allow large tool-call args
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue // ignore malformed lines
		}
		resp, reply := s.handle(req)
		if !reply {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

// handle processes one message; reply is false for notifications (no id).
func (s *server) handle(req request) (resp response, reply bool) {
	if len(req.ID) == 0 {
		return response{}, false // JSON-RPC notification → no response
	}
	resp = response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "nt", "version": s.version},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefs}
	case "tools/call":
		resp.Result = s.callTool(req.Params)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, true
}

type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// callTool dispatches a tools/call. Per MCP, a tool failure is reported as a
// result with isError:true (not a protocol-level error).
func (s *server) callTool(params json.RawMessage) any {
	var p toolCall
	if err := json.Unmarshal(params, &p); err != nil {
		return errResult("bad tool-call params: " + err.Error())
	}
	text, err := s.dispatch(p.Name, p.Arguments)
	if err != nil {
		return errResult(err.Error())
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func errResult(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}

func (s *server) dispatch(name string, a map[string]any) (string, error) {
	switch name {
	case "nt_ready":
		return s.ready(a)
	case "nt_status":
		return s.status(a)
	case "nt_view":
		return s.view(a)
	case "nt_add":
		return s.add(a)
	case "nt_done":
		return s.done(a)
	case "nt_update":
		return s.update(a)
	case "nt_note":
		return s.note(a)
	case "nt_supersede":
		return s.supersede(a)
	case "nt_relink":
		return s.relink(a)
	case "nt_index":
		return s.index(a)
	case "nt_get":
		return s.get(a)
	case "nt_log":
		return s.log(a)
	case "nt_search":
		return s.search(a)
	case "nt_recall":
		return s.recall(a)
	case "nt_links":
		return s.links(a)
	case "nt_mv":
		return s.mv(a)
	case "nt_tag":
		return s.tag(a)
	case "nt_archive":
		return s.archive(a)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// --- tools ---------------------------------------------------------------

func (s *server) ready(a map[string]any) (string, error) {
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	source, tag, project := str(a, "source"), str(a, "tag"), str(a, "project")
	ws := s.workstream(a)
	blocked := task.BlockedIDs(d.Tasks())
	var rows []*task.Task
	for _, t := range d.Tasks() {
		if t.Done || (blocked[t.ID()] && !t.Done) {
			continue // done or dependency-blocked → not ready
		}
		if !workstream.Visible(t.Key("ws"), ws) {
			continue // another agent's parallel work; shared/own tasks stay visible
		}
		if source != "" && t.Source() != source {
			continue
		}
		if tag != "" && !contains(t.Tags(), tag) {
			continue
		}
		if project != "" && !contains(t.Projects(), project) {
			continue
		}
		rows = append(rows, t)
	}
	task.SortByUrgency(rows)
	return jsonText(tasksOut(rows)), nil
}

// view recalls a saved smart view by name through the same view.Apply the CLI
// and web use — the user's own named queries, never re-derived. Without a name
// it lists what's saved, so an agent can discover the views before recalling.
func (s *server) view(a map[string]any) (string, error) {
	views, err := view.Load(s.eng.S.Dir)
	if err != nil {
		return "", err
	}
	name := str(a, "name")
	if name == "" {
		names := make([]string, 0, len(views))
		for n := range views {
			names = append(names, n)
		}
		sort.Strings(names)
		out := make([]map[string]string, 0, len(names))
		for _, n := range names {
			out = append(out, map[string]string{"name": n, "filter": views[n].Summary()})
		}
		return jsonText(map[string]any{"views": out}), nil
	}
	spec, ok := views[name]
	if !ok {
		return "", fmt.Errorf("no saved view %q — call nt_view without a name to list them", name)
	}
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	all := d.Tasks()
	return jsonText(tasksOut(view.Apply(all, spec, task.BlockedIDs(all)))), nil
}

// status synthesizes the state of a project/area in one call, so a resuming
// session doesn't have to stitch together several ready/recall/links calls. It
// reads the store only. doing + blocked come first (that's what a resumer needs
// to unblock), then open by urgency, recent completions, and the notes those
// tasks link to — nt's edge over embedding memory is showing the real WHY.
func (s *server) status(a map[string]any) (string, error) {
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	project, tag := strings.TrimSpace(str(a, "project")), strings.TrimSpace(str(a, "tag"))
	ws := s.workstream(a)
	notes := note.Active(s.listNotes()) // a resuming agent gets the working set, not retired memory
	blocked := task.BlockedIDs(d.Tasks())

	inScope := func(t *task.Task) bool {
		if !workstream.Visible(t.Key("ws"), ws) {
			return false // another agent's parallel work
		}
		if project != "" && !contains(t.Projects(), project) {
			return false
		}
		if tag != "" && !contains(t.Tags(), tag) {
			return false
		}
		return true
	}

	var scoped, doing, blockedT, open []*task.Task
	doneN := 0
	for _, t := range d.Tasks() {
		if !inScope(t) {
			continue
		}
		scoped = append(scoped, t)
		switch {
		case t.Done:
			doneN++
		case t.Status() == "doing":
			doing = append(doing, t)
		case blocked[t.ID()] || t.Status() == "blocked":
			blockedT = append(blockedT, t)
		default:
			open = append(open, t)
		}
	}
	task.SortByUrgency(open)
	openShown := open
	if len(openShown) > 10 {
		openShown = openShown[:10]
	}
	recent := task.CompletedSince(scoped, "") // completed, newest first
	if len(recent) > 5 {
		recent = recent[:5]
	}

	// Linked notes: the notes these tasks point at via [[links]], plus any note
	// carrying the scope tag. This is the context a resuming agent wants.
	seen := map[string]bool{}
	linked := []map[string]string{}
	addNote := func(n *note.Note) {
		if n == nil || seen[n.Path] {
			return
		}
		seen[n.Path] = true
		linked = append(linked, map[string]string{"handle": noteHandle(n), "title": n.Title, "path": n.Rel})
	}
	for _, t := range scoped {
		for _, raw := range t.Links() {
			if it, ok := links.Resolve(raw, d, notes); ok && it.Kind == "note" {
				for _, n := range notes {
					if n.Path == it.Path {
						addNote(n)
						break
					}
				}
			}
		}
	}
	if tag != "" {
		for _, n := range notes {
			if contains(n.Tags, tag) {
				addNote(n)
			}
		}
	}

	scope := "all work"
	switch {
	case project != "" && tag != "":
		scope = "+" + project + " @" + tag
	case project != "":
		scope = "+" + project
	case tag != "":
		scope = "@" + tag
	}

	return jsonText(map[string]any{
		"scope":         scope,
		"counts":        map[string]int{"doing": len(doing), "blocked": len(blockedT), "open": len(open), "done": doneN},
		"doing":         tasksOut(doing),
		"blocked":       tasksOut(blockedT),
		"openByUrgency": tasksOut(openShown),
		"recentlyDone":  tasksOut(recent),
		"linkedNotes":   linked,
	}), nil
}

func (s *server) add(a map[string]any) (string, error) {
	title := strings.TrimSpace(str(a, "text"))
	if title == "" {
		return "", fmt.Errorf("text is required")
	}
	pri, ok := dateparse.Priority(str(a, "priority"))
	if !ok {
		return "", fmt.Errorf("invalid priority %q (use high|med|low)", str(a, "priority"))
	}
	due, ok := dateparse.Date(str(a, "due"))
	if !ok {
		return "", fmt.Errorf("invalid due date %q", str(a, "due"))
	}
	source := str(a, "source")
	if source == "" {
		source = "claude"
	}

	// A task's detail belongs in a linked note ("body"). Three ways here, all
	// filing the note under notes/__tasks__/ so these machine-made notes don't clutter
	// a human's folders:
	//   1. an explicit `body` → keep the short title on the task, body in the note;
	//   2. else a paragraph-length title → split a short clause title off, full
	//      text into the note (the task becomes just the link);
	//   3. else a plain one-line task, no note.
	text := title
	splitNote := ""
	if body := strings.TrimSpace(str(a, "body")); body != "" {
		if n, nerr := note.Create(s.eng.S, title, body, nil, source, note.TaskNoteFolder); nerr == nil {
			text = title + " [[" + n.Title + "]]" // keep the title, link the body
			splitNote = n.Title
		}
	} else if short, full, split := quickadd.SplitLong(title); split {
		if n, nerr := note.Create(s.eng.S, short, full, nil, source, note.TaskNoteFolder); nerr == nil {
			// The task is just a link to the detail note — its short title shows
			// (no duplication), and following it opens the full reasoning.
			text = "[[" + n.Title + "]]"
			splitNote = n.Title
		}
	}
	for _, tg := range strSlice(a, "tags") {
		text += " @" + tg
	}
	if p := str(a, "project"); p != "" {
		text += " +" + p
	}

	var created *task.Task
	err := s.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		t := quickadd.New(text) // normalize inline due:/t:/!pri an agent may emit
		if pri != 0 {
			t.SetPriority(pri)
		}
		if due != "" {
			t.SetKey("due", due)
		}
		t.SetKey("src", source)
		// Normalize the workstream from the resolved identity, unconditionally: this
		// overrides (or strips) any inline `ws:` the agent may have put in the text,
		// so a task can't spoof its way out of — or into — another workstream. "*" is
		// a read-only widen sentinel, never a stored id, so it clears to shared.
		ws := s.workstream(a)
		if ws == "*" {
			ws = ""
		}
		t.SetKey("ws", ws) // empty deletes the key → shared backlog
		if df := str(a, "discovered_from"); df != "" {
			if dt, amb := d.Resolve(df); dt != nil && !amb {
				t.SetKey("discovered", dt.ID())
			}
		}
		d.Append(t)
		rec.Added(t)
		created = t
		return nil
	})
	if err != nil {
		return "", err
	}
	sim := s.similarToTask(created)
	if splitNote != "" {
		hint := "detail saved as the task's linked note under notes/__tasks__/ — following the task opens it."
		if strings.TrimSpace(str(a, "body")) == "" {
			hint = "text was long, so it was split: a short title on the task, the full text in a linked note (notes/__tasks__/). Prefer a short text + a separate body next time."
		}
		res := map[string]any{"task": taskToOut(created), "note": splitNote, "hint": hint}
		if len(sim) > 0 {
			res["similar"] = sim
		}
		return jsonText(res), nil
	}
	// Flat top-level task shape (backward-compatible), plus a non-blocking `similar`
	// list so an agent notices it may be doubling a teammate's task/decision.
	res := toMap(taskToOut(created))
	if len(sim) > 0 {
		res["similar"] = sim
	}
	return jsonText(res), nil
}

// similarToTask finds open tasks (shared project/tag + similar title) and active
// decision notes (shared tag + similar title) that the new task likely duplicates
// — the task-layer analog of the note dedup guard, but advisory (tasks legitimately
// repeat, so this never blocks).
func (s *server) similarToTask(created *task.Task) []map[string]string {
	if created == nil {
		return nil
	}
	title := created.Display()
	out := []map[string]string{}
	if d, err := s.eng.Read(); err == nil {
		for _, t := range d.Tasks() {
			if t.ID() == created.ID() || t.Done {
				continue
			}
			shared := false
			for _, p := range created.Projects() {
				if contains(t.Projects(), p) {
					shared = true
				}
			}
			for _, tg := range created.Tags() {
				if contains(t.Tags(), tg) {
					shared = true
				}
			}
			if shared && note.TitleOverlap(title, t.Display()) >= 0.5 {
				out = append(out, map[string]string{"kind": "task", "id": t.ID(), "text": t.Display()})
			}
		}
	}
	tags := created.Tags()
	for _, n := range note.Active(s.listNotes()) {
		if n.Reserved() {
			continue
		}
		shared := false
		for _, tg := range tags {
			if contains(n.Tags, tg) {
				shared = true
			}
		}
		if shared && note.TitleOverlap(title, n.Title) >= 0.5 {
			out = append(out, map[string]string{"kind": "note", "id": n.ID, "title": n.Title})
		}
	}
	return out
}

func (s *server) done(a map[string]any) (string, error) {
	id, err := requireID(a)
	if err != nil {
		return "", err
	}
	var out *task.Task
	err = s.eng.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		t, e := resolve(d, id)
		if e != nil {
			return e
		}
		mutate.Complete(d, rec, t, mutate.Today()) // spawns next if recurring
		out = t
		return nil
	})
	if err != nil {
		return "", err
	}
	return jsonText(taskToOut(out)), nil
}

func (s *server) update(a map[string]any) (string, error) {
	id, err := requireID(a)
	if err != nil {
		return "", err
	}
	status := str(a, "status")
	priStr := str(a, "priority")
	dueStr := str(a, "due")
	pri, ok := dateparse.Priority(priStr)
	if !ok {
		return "", fmt.Errorf("invalid priority %q", priStr)
	}
	due, ok := dateparse.Date(dueStr)
	if !ok {
		return "", fmt.Errorf("invalid due date %q", dueStr)
	}

	var out *task.Task
	err = s.eng.Apply("update", func(d *task.Doc, rec *mutate.Recorder) error {
		t, e := resolve(d, id)
		if e != nil {
			return e
		}
		rec.Before(t)
		switch status {
		case "":
		case "done":
			mutate.Complete(d, rec, t, mutate.Today())
		case "open":
			t.SetDone(false, "")
			t.SetState("")
		case "doing", "blocked":
			t.SetState(status)
		default:
			return fmt.Errorf("invalid status %q (open|doing|blocked|done)", status)
		}
		if priStr != "" {
			t.SetPriority(pri)
		}
		if due != "" {
			t.SetKey("due", due)
		}
		// Claim/reassign: only an EXPLICIT arg moves a task between workstreams —
		// never the ambient identity, so updating a shared task's status doesn't
		// silently capture it. "*" releases it back to the shared backlog.
		if w, ok := a["workstream"].(string); ok {
			if w = strings.TrimSpace(w); w == "*" {
				w = ""
			}
			t.SetKey("ws", w)
		}
		out = t
		return nil
	})
	if err != nil {
		return "", err
	}
	return jsonText(taskToOut(out)), nil
}

func (s *server) note(a map[string]any) (string, error) {
	title := strings.TrimSpace(str(a, "title"))
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	source := str(a, "source")
	if source == "" {
		source = "claude"
	}
	tags := strSlice(a, "tags")
	supersede := strings.TrimSpace(str(a, "supersede"))

	// Dedup-on-write is a SOFT signal for agents, not a hard refuse: parallel
	// agents legitimately record similar-but-distinct findings at the same time,
	// and refusing would silently DROP a capture (a learning lost). So we always
	// create the note, and if near-duplicates exist we return them in `similar` so
	// the agent can choose to consolidate (nt_supersede/nt_update) on its next turn.
	var similar []*note.Note
	if supersede == "" && !boolArg(a, "force") {
		similar = note.FindSimilar(note.Active(s.listNotes()), title, tags)
	}

	n, err := note.Create(s.eng.S, title, str(a, "body"), tags, source, str(a, "folder"))
	if err != nil {
		return "", err
	}
	if desc := strings.TrimSpace(str(a, "description")); desc != "" {
		n.Extra = append(n.Extra, "description: "+desc)
		if err := n.Save(); err != nil {
			return "", err
		}
	}
	if supersede != "" {
		if err := s.markSuperseded(supersede, n.ID); err != nil {
			return "", err
		}
	}
	// Keep the note's fields at the top level (backward-compatible), adding
	// danglingLinks / superseded only when relevant.
	res := toMap(noteToOut(n))
	if dangling := s.danglingLinks(n); len(dangling) > 0 {
		res["danglingLinks"] = dangling // bad outbound [[refs]] to fix now
	}
	if len(similar) > 0 {
		sims := make([]map[string]string, 0, len(similar))
		for _, sn := range similar {
			sims = append(sims, map[string]string{"id": sn.ID, "title": sn.Title})
		}
		// Created anyway (no data loss); consider consolidating if it's a true dup.
		res["similar"] = sims
	}
	if supersede != "" {
		res["superseded"] = supersede
	}
	return jsonText(res), nil
}

// toMap flattens a struct to a map via its JSON tags, so extra keys can be added
// to a response without losing the struct's top-level shape.
func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// supersede marks one note as replaced by another (nt_supersede).
func (s *server) supersede(a map[string]any) (string, error) {
	oldH := strings.TrimSpace(str(a, "handle"))
	by := strings.TrimSpace(str(a, "by"))
	if oldH == "" || by == "" {
		return "", fmt.Errorf("handle and by are required (the old note, and the note that replaces it)")
	}
	notes := s.listNotes()
	newNote, ok := resolveNoteMCP(notes, by)
	if !ok {
		return "", fmt.Errorf("no note %q (by)", by)
	}
	if err := s.markSuperseded(oldH, newNote.ID); err != nil {
		return "", err
	}
	return jsonText(map[string]any{"superseded": oldH, "canonical": newNote.ID}), nil
}

// relink rewrites a wrong outbound [[link]] inside a note's body (nt_relink).
func (s *server) relink(a map[string]any) (string, error) {
	handle := strings.TrimSpace(str(a, "handle"))
	oldT := strings.TrimSpace(str(a, "old"))
	newT := strings.TrimSpace(str(a, "new"))
	if handle == "" || oldT == "" || newT == "" {
		return "", fmt.Errorf("handle, old, and new are required")
	}
	notes := s.listNotes()
	n, ok := resolveNoteMCP(notes, handle)
	if !ok {
		return "", fmt.Errorf("no note %q", handle)
	}
	if _, ok := links.Resolve(newT, nil, notes); !ok {
		return "", fmt.Errorf("new target [[%s]] doesn't resolve to any note", newT)
	}
	body, count := relinkBody(n.Body, oldT, newT)
	if count == 0 {
		return "", fmt.Errorf("no [[%s]] found in note %q", oldT, handle)
	}
	n.Body = body
	if err := n.Save(); err != nil {
		return "", err
	}
	return jsonText(map[string]any{"id": n.ID, "relinked": count, "from": oldT, "to": newT}), nil
}

// relinkBody rewrites [[oldT]], [[oldT|alias]] and [[oldT#frag]] to newT.
func relinkBody(body, oldT, newT string) (string, int) {
	count := 0
	for _, suffix := range []string{"]]", "|", "#"} {
		from := "[[" + oldT + suffix
		count += strings.Count(body, from)
		body = strings.ReplaceAll(body, from, "[["+newT+suffix)
	}
	return body, count
}

// markSuperseded stamps oldHandle's note with superseded_by=newID.
func (s *server) markSuperseded(oldHandle, newID string) error {
	old, ok := resolveNoteMCP(s.listNotes(), oldHandle)
	if !ok {
		return fmt.Errorf("no note %q to supersede", oldHandle)
	}
	if old.ID == newID {
		return fmt.Errorf("a note can't supersede itself")
	}
	old.SupersededBy = newID
	return old.Save()
}

// danglingLinks returns the [[links]] in a note's body that don't resolve.
func (s *server) danglingLinks(n *note.Note) []string {
	if !strings.Contains(n.Body, "[[") {
		return nil
	}
	d, _ := s.eng.Read()
	notes := s.listNotes()
	var out []string
	for _, raw := range links.Wikilinks(n.Body) {
		if _, ok := links.Resolve(raw, d, notes); !ok {
			out = append(out, raw)
		}
	}
	return out
}

// resolveNoteMCP resolves a handle to a note (id/slug/title), nil if not found.
func resolveNoteMCP(notes []*note.Note, handle string) (*note.Note, bool) {
	it, ok := links.Resolve(handle, nil, notes)
	if !ok || it.Kind != "note" {
		return nil, false
	}
	for _, n := range notes {
		if n.Path == it.Path {
			return n, true
		}
	}
	return nil, false
}

// index is the progressive-disclosure entry point: a compact catalog of the
// knowledge base — one stub per note (id, title, one-line description, tags,
// folder), NO bodies — plus the active (open+doing, unblocked) task list. An agent
// loads this cheaply to resume context, then fetches only the notes it needs by id
// (nt_get) or nt_search. It replaces the old bulk recall, which returned every note
// body and grew linearly with the whole corpus.
func (s *server) index(a map[string]any) (string, error) {
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	tag, folder := strings.TrimSpace(str(a, "tag")), strings.Trim(strings.TrimSpace(str(a, "folder")), "/")
	since := ""
	if s := strings.TrimSpace(str(a, "updated_since")); s != "" {
		if d, ok := dateparse.Date(s); ok {
			since = d
		}
	}
	notes := note.Active(s.listNotes())
	stubs := make([]noteStub, 0, len(notes))
	for _, n := range notes {
		if n.Reserved() {
			continue // task-detail notes aren't part of the KB catalog
		}
		if folder != "" && !strings.HasPrefix(n.Rel, folder+"/") {
			continue
		}
		if tag != "" && !contains(n.Tags, tag) {
			continue
		}
		if since != "" && noteChangedDate(n) < since {
			continue // "what changed since T"
		}
		stubs = append(stubs, noteToStub(n, ""))
	}
	sort.SliceStable(stubs, func(i, j int) bool {
		if stubs[i].Folder != stubs[j].Folder {
			return stubs[i].Folder < stubs[j].Folder
		}
		return stubs[i].Title < stubs[j].Title
	})
	noteTotal := len(stubs)
	if lim := intArg(a, "limit"); lim > 0 && len(stubs) > lim {
		stubs = stubs[:lim]
	}

	ws := s.workstream(a)
	blocked := task.BlockedIDs(d.Tasks())
	var active, scoped []*task.Task
	for _, t := range d.Tasks() {
		if !workstream.Visible(t.Key("ws"), ws) {
			continue
		}
		if tag != "" && !contains(t.Tags(), tag) {
			continue
		}
		scoped = append(scoped, t)
		if !t.Done && !blocked[t.ID()] {
			active = append(active, t)
		}
	}
	task.SortByUrgency(active)
	// A few recent completions so a resuming agent sees what's already handled —
	// not just what's open (the active list omits done tasks).
	recent := task.CompletedSince(scoped, "")
	if len(recent) > 5 {
		recent = recent[:5]
	}
	// Compact mode: one line per note/task instead of JSON. The session-start
	// nt_index call is the dominant recurring token cost once the store matures;
	// compact drops the JSON scaffolding (keys, braces, quotes) an agent doesn't
	// need to read the catalog. Opt in with format:"compact".
	if str(a, "format") == "compact" {
		return compactIndex(stubs, active, recent, noteTotal), nil
	}
	out := map[string]any{
		"notes": stubs, "tasks": tasksOut(active), "recentlyDone": tasksOut(recent),
	}
	if noteTotal > len(stubs) {
		out["truncated"] = true
		out["noteTotal"] = noteTotal
	}
	return jsonText(out), nil
}

// compactIndex renders the KB catalog as terse plain text — one line per note
// (shortid · title — description · @tags) and per active task — for the
// session-start nt_index call. Much cheaper than JSON for the same information.
func compactIndex(stubs []noteStub, active, recent []*task.Task, noteTotal int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NOTES (%d)\n", noteTotal)
	for _, n := range stubs {
		fmt.Fprintf(&b, "%s  %s", shortID(n.ID), n.Title)
		if n.Description != "" {
			fmt.Fprintf(&b, " — %s", n.Description)
		}
		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, "  @%s", strings.Join(n.Tags, " @"))
		}
		b.WriteByte('\n')
	}
	if noteTotal > len(stubs) {
		fmt.Fprintf(&b, "… %d more (pass a higher limit or a tag/folder filter)\n", noteTotal-len(stubs))
	}
	fmt.Fprintf(&b, "\nTASKS (%d open)\n", len(active))
	for _, t := range active {
		fmt.Fprintf(&b, "%s  [%s] %s", shortID(t.ID()), t.Status(), t.Text)
		if due := t.Due(); due != "" {
			fmt.Fprintf(&b, " (due %s)", due)
		}
		b.WriteByte('\n')
	}
	if len(recent) > 0 {
		b.WriteString("\nRECENTLY DONE\n")
		for _, t := range recent {
			fmt.Fprintf(&b, "%s  %s\n", shortID(t.ID()), t.Text)
		}
	}
	return b.String()
}

// get fetches one note's full content by handle (id/slug/title) — the on-demand
// half of progressive disclosure. With `section`, it returns only the markdown
// block under the matching heading, so an agent can pull a slice, not the whole
// note.
func (s *server) get(a map[string]any) (string, error) {
	handle := strings.TrimSpace(str(a, "handle"))
	if handle == "" {
		return "", fmt.Errorf("handle is required (a note id, slug, or title)")
	}
	notes := s.listNotes() // refreshes the cache's id index used by ByID below
	// Fast path: a bare id resolves via the cache's id index in O(1), skipping the
	// slug/title resolve scan. Falls through to Resolve for slug/title handles.
	var n *note.Note
	if s.cache != nil {
		n = s.cache.ByID(handle)
	}
	if n == nil {
		it, ok := links.Resolve(handle, nil, notes)
		if !ok || it.Kind != "note" {
			return "", fmt.Errorf("no note %q", handle)
		}
		for _, cand := range notes {
			if cand.Path == it.Path {
				n = cand
				break
			}
		}
	}
	if n == nil {
		return "", fmt.Errorf("no note %q", handle)
	}
	body := strings.TrimSpace(n.Body)
	if section := strings.TrimSpace(str(a, "section")); section != "" {
		body = extractSection(n.Body, section)
		if body == "" {
			return "", fmt.Errorf("no section %q in note %q", section, handle)
		}
	}
	return jsonText(map[string]any{
		"id": n.ID, "title": n.Title, "tags": n.Tags, "folder": pathDir(n.Rel),
		"source": n.Source, "body": body,
	}), nil
}

func (s *server) log(a map[string]any) (string, error) {
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	bound := str(a, "since")
	if days := intArg(a, "days"); days > 0 {
		if cut := time.Now().AddDate(0, 0, -days).Format("2006-01-02"); bound == "" || cut > bound {
			bound = cut
		}
	}
	done := task.CompletedSince(d.Tasks(), bound)
	source, ws := str(a, "source"), s.workstream(a)
	if source != "" || (ws != "" && ws != "*") {
		kept := done[:0]
		for _, t := range done {
			if source != "" && t.Source() != source {
				continue
			}
			if !workstream.Visible(t.Key("ws"), ws) {
				continue
			}
			kept = append(kept, t)
		}
		done = kept
	}
	return jsonText(tasksOut(done)), nil
}

// search is the KB's on-demand retrieval verb. It returns ranked STUBS (id,
// title, one-line description, a query-context snippet, tags, folder) — NOT full
// bodies — so a broad query costs a few hundred tokens, not the whole corpus. The
// agent picks an id and fetches it with nt_get. Ranking: title matches first, then
// body matches; results are hard-capped by `limit` (default 8). Pass full=true to
// get bodies inline for the (still limited) hits.
func (s *server) search(a map[string]any) (string, error) {
	q := strings.TrimSpace(str(a, "query"))
	tag := strings.TrimSpace(str(a, "tag"))
	if q == "" && tag == "" {
		return "", fmt.Errorf("query or tag is required")
	}
	typ := str(a, "type")
	if typ == "" {
		typ = "all"
	}
	limit := intArg(a, "limit")
	if limit <= 0 {
		limit = 8
	}
	full := boolArg(a, "full")
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	notes := note.Active(s.listNotes())
	ql := strings.ToLower(q)

	type scored struct {
		n       *note.Note
		snippet string
		rank    int // 0 = title match, 1 = body/tag match — title ranks first
	}
	var hits []scored
	if typ == "all" || typ == "note" {
		for _, n := range notes {
			if n.Reserved() {
				continue // task-detail / machine notes aren't part of the KB catalog
			}
			if tag != "" && !contains(n.Tags, tag) {
				continue
			}
			// Match title first (rank 0), then the BODY only (rank 1). Scanning the
			// parsed body — not the raw file — means a query never matches frontmatter
			// (so a tag like `auth` no longer masquerades as a body hit), and the
			// snippet is a real excerpt around the term, not a `tags: […]` line.
			switch {
			case q == "" || strings.Contains(strings.ToLower(n.Title), ql):
				hits = append(hits, scored{n, "", 0})
			default:
				if snip := bodySnippet(n.Body, ql); snip != "" {
					hits = append(hits, scored{n, snip, 1})
				}
			}
		}
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].rank != hits[j].rank {
				return hits[i].rank < hits[j].rank
			}
			return hits[i].n.Rel < hits[j].n.Rel
		})
	}
	noteTotal := len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	var noteRes any
	if full {
		arr := make([]noteOut, 0, len(hits))
		for _, h := range hits {
			arr = append(arr, noteToOut(h.n))
		}
		noteRes = arr
	} else {
		arr := make([]noteStub, 0, len(hits))
		for _, h := range hits {
			arr = append(arr, noteToStub(h.n, h.snippet))
		}
		noteRes = arr
	}

	var tout []taskOut
	taskTotal := 0
	if typ == "all" || typ == "task" {
		ws := s.workstream(a)
		for _, t := range d.Tasks() {
			// Scope tasks to the caller's workstream, exactly as nt_index/nt_ready
			// do — otherwise a parallel agent's search leaks every other agent's
			// in-flight tasks. Notes stay store-wide (knowledge is shared).
			if !workstream.Visible(t.Key("ws"), ws) {
				continue
			}
			if tag != "" && !contains(t.Tags(), tag) {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(t.Text), ql) {
				taskTotal++
				if len(tout) < limit {
					tout = append(tout, taskToOut(t))
				}
			}
		}
	}
	out := map[string]any{"notes": noteRes, "tasks": tout}
	if noteTotal > len(hits) || taskTotal > len(tout) {
		// Never cap silently: tell the agent more exist so it can narrow the query.
		out["truncated"] = true
		out["totals"] = map[string]int{"notes": noteTotal, "tasks": taskTotal, "limit": limit}
	}
	return jsonText(out), nil
}

// recall ranks notes by relevance to a free-text task context — lessons/gotchas
// first — so a resuming session reads what a past session learned before repeating
// the mistake. Unlike search's substring-AND, it stems and synonym-expands the
// context so a paraphrase still finds the note. This is the proactive half of the
// learn-from-sessions loop; the agent calls it at task start.
func (s *server) recall(a map[string]any) (string, error) {
	context := strings.TrimSpace(str(a, "context"))
	if context == "" {
		return "", fmt.Errorf("context is required: describe what you're about to work on")
	}
	limit := intArg(a, "limit")
	if limit <= 0 {
		limit = 8
	}
	notes := note.Active(s.listNotes())
	if boolArg(a, "lessons_only") {
		kept := notes[:0]
		for _, n := range notes {
			if contains(n.Tags, recall.LessonTag) {
				kept = append(kept, n)
			}
		}
		notes = kept
	}
	results := recall.Rank(notes, context, limit)
	stubs := make([]map[string]any, 0, len(results))
	for _, r := range results {
		stubs = append(stubs, map[string]any{
			"id": r.Note.ID, "title": r.Note.Title, "description": r.Note.Description(160),
			"tags": r.Note.Tags, "folder": pathDir(r.Note.Rel), "lesson": r.Lesson,
		})
	}
	return jsonText(map[string]any{"results": stubs}), nil
}

func (s *server) links(a map[string]any) (string, error) {
	handle := strings.TrimSpace(str(a, "handle"))
	if handle == "" {
		return "", fmt.Errorf("handle is required")
	}
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	notes := s.listNotes()

	forward := func(raws []string) []linkOut {
		out := make([]linkOut, 0, len(raws))
		for _, raw := range raws {
			if it, ok := links.Resolve(raw, d, notes); ok {
				out = append(out, linkOut{Kind: it.Kind, ID: it.ID, Title: it.Title})
			} else {
				out = append(out, linkOut{Kind: "unresolved", Target: raw})
			}
		}
		return out
	}

	if t, terr := resolve(d, handle); terr == nil {
		return jsonText(map[string]any{
			"handle": t.ID(), "kind": "task", "title": t.Text,
			"forward": forward(t.Links()), "backlinks": backlinksOut(links.Backlinks(s.eng.S, t.ID(), ""), notes),
		}), nil
	}
	if it, ok := links.Resolve(handle, d, notes); ok && it.Kind == "note" {
		for _, n := range notes {
			if n.Path == it.Path {
				return jsonText(map[string]any{
					"handle": noteHandle(n), "kind": "note", "title": n.Title,
					"forward": forward(links.Wikilinks(n.Body)), "backlinks": backlinksOut(links.Backlinks(s.eng.S, n.ID, n.Rel), notes),
				}), nil
			}
		}
	}
	return "", fmt.Errorf("no task or note %q", handle)
}

func (s *server) mv(a map[string]any) (string, error) {
	handle := strings.TrimSpace(str(a, "handle"))
	dest := strings.TrimSpace(str(a, "dest"))
	if handle == "" || dest == "" {
		return "", fmt.Errorf("handle and dest are required")
	}
	notes, _ := note.List(s.eng.S)
	it, ok := links.Resolve(handle, nil, notes)
	if !ok || it.Kind != "note" {
		if it.Kind == "ambiguous" {
			return "", fmt.Errorf("%q is ambiguous (%s) — qualify with a folder", handle, it.Title)
		}
		return "", fmt.Errorf("no note %q", handle)
	}
	var src *note.Note
	for _, n := range notes {
		if n.Path == it.Path {
			src = n
			break
		}
	}
	newRel, updated, err := s.eng.RenameNote(src, notes, dest)
	if err != nil {
		return "", err
	}
	return jsonText(map[string]any{"moved_to": newRel, "updated_refs": updated}), nil
}

func (s *server) tag(a map[string]any) (string, error) {
	handle := strings.TrimSpace(str(a, "handle"))
	add, remove := strSlice(a, "add"), strSlice(a, "remove")
	if handle == "" || (len(add) == 0 && len(remove) == 0) {
		return "", fmt.Errorf("handle and at least one of add/remove are required")
	}
	notes, _ := note.List(s.eng.S)
	it, ok := links.Resolve(handle, nil, notes)
	if !ok || it.Kind != "note" {
		return "", fmt.Errorf("no note %q", handle)
	}
	var n *note.Note
	for _, x := range notes {
		if x.Path == it.Path {
			n = x
			break
		}
	}
	for _, tg := range add {
		if tg = strings.TrimPrefix(tg, "@"); tg != "" && !contains(n.Tags, tg) {
			n.Tags = append(n.Tags, tg)
		}
	}
	for _, tg := range remove {
		n.Tags = removeTag(n.Tags, strings.TrimPrefix(tg, "@"))
	}
	n.Updated = time.Now().Format(time.RFC3339)
	if err := n.Save(); err != nil {
		return "", err
	}
	return jsonText(noteToOut(n)), nil
}

// archive retires a note from the agent's working set (recall/search/status) by
// flipping the soft `archived` frontmatter flag, or restores it with undo. The
// file stays on disk with its links intact — the same reversible retire the CLI
// and web offer. It resolves against all notes (archived included) so an already
// retired note can be found to unarchive.
func (s *server) archive(a map[string]any) (string, error) {
	handle := strings.TrimSpace(str(a, "handle"))
	if handle == "" {
		return "", fmt.Errorf("handle is required")
	}
	unarchive := boolArg(a, "undo")
	notes, _ := note.List(s.eng.S)
	it, ok := links.Resolve(handle, nil, notes)
	if !ok || it.Kind != "note" {
		return "", fmt.Errorf("no note %q", handle)
	}
	var n *note.Note
	for _, x := range notes {
		if x.Path == it.Path {
			n = x
			break
		}
	}
	n.Archived = !unarchive
	n.Updated = time.Now().Format(time.RFC3339)
	if err := n.Save(); err != nil {
		return "", err
	}
	return jsonText(noteToOut(n)), nil
}

func removeTag(ss []string, want string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != want {
			out = append(out, s)
		}
	}
	return out
}

// --- output shapes (id-first; no positional index — agents use ids) ------

type taskOut struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Status     string   `json:"status"`
	Priority   string   `json:"priority,omitempty"`
	Due        string   `json:"due,omitempty"`
	Completed  string   `json:"completed,omitempty"`
	Project    string   `json:"project,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Source     string   `json:"source,omitempty"`
	Workstream string   `json:"workstream,omitempty"`
	Discovered string   `json:"discovered_from,omitempty"`
}

func taskToOut(t *task.Task) taskOut {
	o := taskOut{
		ID: t.ID(), Text: t.Text, Status: t.Status(), Due: t.Due(),
		Completed: t.Completed, Tags: t.Tags(), Source: t.Source(),
		Workstream: t.Key("ws"), Discovered: t.Discovered(),
	}
	if t.Priority != 0 {
		o.Priority = string(t.Priority)
	}
	if p := t.Projects(); len(p) > 0 {
		o.Project = p[0]
	}
	return o
}

func tasksOut(ts []*task.Task) []taskOut {
	out := make([]taskOut, 0, len(ts))
	for _, t := range ts {
		out = append(out, taskToOut(t))
	}
	return out
}

type noteOut struct {
	ID       string   `json:"id,omitempty"`
	Rel      string   `json:"rel,omitempty"` // path handle (notes authored outside nt have no id)
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Source   string   `json:"source,omitempty"`
	Archived bool     `json:"archived,omitempty"` // retired from search/status
	Body     string   `json:"body,omitempty"`
}

func noteToOut(n *note.Note) noteOut {
	return noteOut{ID: n.ID, Rel: n.Rel, Title: n.Title, Tags: n.Tags, Source: n.Source, Archived: n.Archived, Body: strings.TrimSpace(n.Body)}
}

// noteStub is a note WITHOUT its body — the progressive-disclosure unit returned
// by nt_index and nt_search. The agent reads the stub (id + description + snippet)
// to decide whether to nt_get the full note.
type noteStub struct {
	ID          string   `json:"id,omitempty"`
	Rel         string   `json:"rel,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Snippet     string   `json:"snippet,omitempty"` // query-context line (search only)
	Tags        []string `json:"tags,omitempty"`
	Folder      string   `json:"folder,omitempty"`
	Source      string   `json:"source,omitempty"` // author/agent — ownership on a shared store
	Updated     string   `json:"updated,omitempty"`
}

func noteToStub(n *note.Note, snippet string) noteStub {
	upd := n.Updated
	if upd == "" {
		upd = n.Created
	}
	return noteStub{
		ID: n.ID, Rel: n.Rel, Title: n.Title, Description: n.Description(160),
		Snippet: snippet, Tags: n.Tags, Folder: pathDir(n.Rel), Source: n.Source, Updated: shortDate(upd),
	}
}

// shortID is the last 6 chars of a ULID — the human-facing handle nt prints
// (the entropy tail; the timestamp prefix is shared by same-second ids).
func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

// noteChangedDate is a note's effective change date (YYYY-MM-DD): the later of its
// file mtime (catches external edits) and its frontmatter updated/created.
func noteChangedDate(n *note.Note) string {
	upd := n.Updated
	if upd == "" {
		upd = n.Created
	}
	d := shortDate(upd)
	if !n.ModTime.IsZero() {
		if m := n.ModTime.Format("2006-01-02"); m > d {
			d = m
		}
	}
	return d
}

// pathDir is the folder part of a notes/-relative path ("" for a root note).
func pathDir(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}

// bodySnippet returns the first body line containing ql (lowercased query),
// collapsed and clamped — a real excerpt around the match. "" if no line matches
// or ql is empty. Headings are skipped so the snippet is prose, not a "# Title".
func bodySnippet(body, ql string) string {
	if ql == "" {
		return ""
	}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(strings.ToLower(line), ql) {
			return clipLine(line, 160)
		}
	}
	return ""
}

// clipLine collapses whitespace and truncates to one line ≤max chars.
func clipLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max-1]) + "…"
	}
	return s
}

// extractSection returns the markdown block under the heading whose text matches
// name (case-insensitive), up to the next heading of the same or higher level.
// Returns "" if no such heading exists.
func extractSection(body, name string) string {
	lines := strings.Split(body, "\n")
	want := strings.ToLower(strings.TrimSpace(name))
	start, level := -1, 0
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		h := strings.TrimSpace(strings.TrimLeft(t, "#"))
		lv := len(t) - len(strings.TrimLeft(t, "#"))
		if strings.ToLower(h) == want {
			start, level = i, lv
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "#") {
			lv := len(t) - len(strings.TrimLeft(t, "#"))
			if lv <= level {
				end = i
				break
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

// linkOut is one forward link in the nt_links result.
type linkOut struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Target string `json:"target,omitempty"` // raw [[…]] when unresolved
}

// noteHandle is a note's id, or its path when authored outside nt (no id).
func noteHandle(n *note.Note) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Rel
}

// backlinksOut maps backlink hits to notes (linkable) or task lines.
func backlinksOut(hits []search.Hit, notes []*note.Note) []map[string]string {
	byPath := make(map[string]*note.Note, len(notes))
	for _, n := range notes {
		byPath[n.Path] = n
	}
	seen := map[string]bool{}
	out := []map[string]string{}
	for _, h := range hits {
		if n, ok := byPath[h.Path]; ok {
			if n.Archived {
				continue // a retired note is out of the graph — no backlink from it
			}
			hk := noteHandle(n)
			if seen[hk] {
				continue
			}
			seen[hk] = true
			out = append(out, map[string]string{"kind": "note", "handle": hk, "title": n.Title})
		} else {
			// A task backlink: parse the raw todo.txt line into a clean reference
			// (short id + display text) instead of leaking `id:<ULID>`/link markup.
			row := map[string]string{"kind": "task", "text": strings.TrimSpace(h.Text)}
			if t, ok := task.ParseLine(h.Text); ok && t.ID() != "" {
				row["id"] = shortID(t.ID())
				row["text"] = t.Display()
			}
			out = append(out, row)
		}
	}
	return out
}

// --- small helpers -------------------------------------------------------

func resolve(d *task.Doc, id string) (*task.Task, error) {
	t, amb := d.Resolve(id)
	if amb {
		return nil, fmt.Errorf("%q is ambiguous", id)
	}
	if t == nil {
		return nil, fmt.Errorf("no task %q", id)
	}
	return t, nil
}

func requireID(a map[string]any) (string, error) {
	id := strings.TrimSpace(str(a, "id"))
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if task.IsPositional(id) {
		return "", fmt.Errorf("use the stable task id, not a positional handle (%q)", id)
	}
	return id, nil
}

// jsonText serializes a tool result. MCP payloads are consumed by the model, not
// read by a human, so we emit COMPACT JSON (no indentation) — the 2-space pretty
// print was ~24% pure whitespace on every read, billed on every retrieval.
func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("error encoding result: %v", err)
	}
	return string(b)
}

func str(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case []any:
		// Tolerate an LLM passing an array (e.g. tag: ["auth"]) where a string is
		// expected. Fall back to the FIRST string element — these single-value args
		// (tag/folder) take one value, so this still filters. Without it the type
		// assertion silently fails, the filter no-ops, and nt_index/nt_search return
		// the FULL store — the worst-case token blast from a natural arg shape.
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

func strSlice(a map[string]any, k string) []string {
	v, ok := a[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, e := range v {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func intArg(a map[string]any, k string) int {
	if f, ok := a[k].(float64); ok {
		return int(f)
	}
	return 0
}

func boolArg(a map[string]any, k string) bool {
	b, _ := a[k].(bool)
	return b
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
