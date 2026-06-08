package task

import "strings"

// Node is one line of the file: a parsed Task, or a preserved raw line (blank
// lines, comments, anything that isn't a task). Exactly one field is set.
type Node struct {
	Task *Task
	Raw  string // used when Task == nil
}

// Doc is an ordered list of nodes — the in-memory model of a tasks file. It
// preserves line order and the file's trailing-newline state so an unmodified
// document renders byte-identically (SPEC §4).
type Doc struct {
	Nodes      []Node
	trailingNL bool
}

// Parse builds a Doc from raw file bytes.
func Parse(data []byte) *Doc {
	d := &Doc{}
	s := string(data)
	if s == "" {
		return d
	}
	d.trailingNL = strings.HasSuffix(s, "\n")
	body := s
	if d.trailingNL {
		body = body[:len(body)-1]
	}
	for _, line := range strings.Split(body, "\n") {
		if t, ok := parseLine(line); ok {
			d.Nodes = append(d.Nodes, Node{Task: t})
		} else {
			d.Nodes = append(d.Nodes, Node{Raw: line})
		}
	}
	return d
}

// Render serializes the Doc back to file bytes.
func (d *Doc) Render() []byte {
	var sb strings.Builder
	for i, n := range d.Nodes {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if n.Task != nil {
			sb.WriteString(n.Task.Line())
		} else {
			sb.WriteString(n.Raw)
		}
	}
	if d.trailingNL {
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

// Tasks returns the parsed tasks in document order.
func (d *Doc) Tasks() []*Task {
	var out []*Task
	for _, n := range d.Nodes {
		if n.Task != nil {
			out = append(out, n.Task)
		}
	}
	return out
}

// Append adds a task as a new node. If the document is non-empty and lacks a
// trailing newline, one is added first so the new task starts on its own line.
func (d *Doc) Append(t *Task) {
	if len(d.Nodes) > 0 {
		d.trailingNL = true
	}
	d.Nodes = append(d.Nodes, Node{Task: t})
}

// FindByID returns the task with the given ULID, or nil.
func (d *Doc) FindByID(id string) *Task {
	for _, n := range d.Nodes {
		if n.Task != nil && n.Task.ID() == id {
			return n.Task
		}
	}
	return nil
}

// Resolve maps a user-supplied handle to a task. It accepts a full ULID, the
// displayed trailing short code (id[len-6:]), or a 1-based positional "task:N"
// reference (interactive-only, best-effort — SPEC §7.2). ambiguous is true when
// a short code matches more than one task.
func (d *Doc) Resolve(handle string) (t *Task, ambiguous bool) {
	tasks := d.Tasks()
	if n, ok := parsePositional(handle); ok {
		if n >= 1 && n <= len(tasks) {
			return tasks[n-1], false
		}
		return nil, false
	}
	h := strings.ToUpper(handle)
	var matches []*Task
	for _, t := range tasks {
		id := t.ID()
		if id == h {
			return t, false
		}
		// Match the trailing short code only — that's the handle nt displays
		// (id[len-6:]); a copied full id matches exactly above. ULIDs share a long
		// leading timestamp prefix, so prefix-matching is useless (never shown)
		// and unsafe: a short handle could prefix-match a task the user never
		// meant, or make their real (suffix) handle look ambiguous (F5/C5).
		if strings.HasSuffix(id, h) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], false
	case 0:
		return nil, false
	default:
		return nil, true
	}
}

// IsPositional reports whether a handle is a positional "task:N" / "N" reference
// rather than a stable ULID — so adapters can refuse it from non-interactive
// callers, where the list index may have shifted between read and act (§7.2).
func IsPositional(handle string) bool {
	_, ok := parsePositional(handle)
	return ok
}

// parsePositional parses "task:N" / "N" into a 1-based index.
func parsePositional(h string) (int, bool) {
	h = strings.TrimPrefix(h, "task:")
	if h == "" {
		return 0, false
	}
	n := 0
	for _, c := range h {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// ReplaceByID swaps in a new task for the node with the given id, returning
// false if no such task exists.
func (d *Doc) ReplaceByID(id string, t *Task) bool {
	for i, n := range d.Nodes {
		if n.Task != nil && n.Task.ID() == id {
			d.Nodes[i].Task = t
			return true
		}
	}
	return false
}

// Remove deletes the node holding the task with id, returning the removed
// task's raw line (its before-image) for the undo journal.
func (d *Doc) Remove(id string) (before string, ok bool) {
	for i, n := range d.Nodes {
		if n.Task != nil && n.Task.ID() == id {
			before = n.Task.Line()
			d.Nodes = append(d.Nodes[:i], d.Nodes[i+1:]...)
			return before, true
		}
	}
	return "", false
}
