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
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

const protocolVersion = "2024-11-05"

// Serve runs the MCP stdio loop against the global store until stdin closes.
func Serve(version string) error {
	e, err := mutate.Open()
	if err != nil {
		return err
	}
	return (&server{eng: e, version: version}).loop(os.Stdin, os.Stdout)
}

type server struct {
	eng     *mutate.Engine
	version string
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
	case "nt_add":
		return s.add(a)
	case "nt_done":
		return s.done(a)
	case "nt_update":
		return s.update(a)
	case "nt_note":
		return s.note(a)
	case "nt_recall":
		return s.recall(a)
	case "nt_log":
		return s.log(a)
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
	blocked := task.BlockedIDs(d.Tasks())
	var rows []*task.Task
	for _, t := range d.Tasks() {
		if t.Done || (blocked[t.ID()] && !t.Done) {
			continue // done or dependency-blocked → not ready
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
	text := title
	for _, tg := range strSlice(a, "tags") {
		text += " @" + tg
	}
	if p := str(a, "project"); p != "" {
		text += " +" + p
	}

	var created *task.Task
	err := s.eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		t := task.New(text)
		if pri != 0 {
			t.SetPriority(pri)
		}
		if due != "" {
			t.SetKey("due", due)
		}
		t.SetKey("src", source)
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
	return jsonText(taskToOut(created)), nil
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
	n, err := note.Create(s.eng.S, title, str(a, "body"), strSlice(a, "tags"), source, str(a, "folder"))
	if err != nil {
		return "", err
	}
	return jsonText(noteToOut(n)), nil
}

func (s *server) recall(a map[string]any) (string, error) {
	d, err := s.eng.Read()
	if err != nil {
		return "", err
	}
	source, since := str(a, "source"), str(a, "since")
	var tasks []*task.Task
	for _, t := range d.Tasks() {
		if source != "" && t.Source() != source {
			continue
		}
		if since != "" && t.Created != "" && t.Created < since {
			continue
		}
		tasks = append(tasks, t)
	}
	notes, _ := note.List(s.eng.S)
	var nout []noteOut
	for _, n := range notes {
		if source != "" && n.Source != source {
			continue
		}
		if since != "" && n.Created != "" && shortDate(n.Created) < since {
			continue
		}
		nout = append(nout, noteToOut(n))
	}
	return jsonText(map[string]any{"tasks": tasksOut(tasks), "notes": nout}), nil
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
	if source := str(a, "source"); source != "" {
		kept := done[:0]
		for _, t := range done {
			if t.Source() == source {
				kept = append(kept, t)
			}
		}
		done = kept
	}
	return jsonText(tasksOut(done)), nil
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
	Discovered string   `json:"discovered_from,omitempty"`
}

func taskToOut(t *task.Task) taskOut {
	o := taskOut{
		ID: t.ID(), Text: t.Text, Status: t.Status(), Due: t.Due(),
		Completed: t.Completed, Tags: t.Tags(), Source: t.Source(), Discovered: t.Discovered(),
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
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Tags   []string `json:"tags,omitempty"`
	Source string   `json:"source,omitempty"`
	Body   string   `json:"body,omitempty"`
}

func noteToOut(n *note.Note) noteOut {
	return noteOut{ID: n.ID, Title: n.Title, Tags: n.Tags, Source: n.Source, Body: strings.TrimSpace(n.Body)}
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

func jsonText(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("error encoding result: %v", err)
	}
	return string(b)
}

func str(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
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
