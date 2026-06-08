// Package task implements nt's todo.txt task model with the lossless
// round-trip guarantee from SPEC §4: a file is parsed into an ordered list of
// nodes (each a parsed task or a preserved raw line), and an unmodified line is
// re-emitted byte-for-byte. Only when a task is mutated do we re-render it from
// structured fields, touching only the tokens nt owns and preserving any
// unknown key:value tokens another todo.txt client may have written.
package task

import (
	"regexp"
	"strings"

	"github.com/navbytes/nt/internal/ulid"
)

// kv is one key:value token (e.g. due:2026-06-05). Unknown keys are preserved.
type kv struct {
	Key, Val string
}

// Task is a single todo.txt line, parsed.
type Task struct {
	raw   string // authoritative rendering while !dirty
	dirty bool

	Done      bool
	Priority  byte   // 0, or 'A'..'Z'
	Completed string // YYYY-MM-DD or ""
	Created   string // YYYY-MM-DD or ""
	Text      string // description, including inline +project / @tag / [[link]]

	kvs []kv // all key:value tokens, in original order
}

var (
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	keyRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	linkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

func isDate(s string) bool { return dateRe.MatchString(s) }

func isPriority(s string) bool {
	return len(s) == 3 && s[0] == '(' && s[2] == ')' && s[1] >= 'A' && s[1] <= 'Z'
}

// splitKV reports whether tok is a key:value token nt should preserve as
// structured metadata. URLs (value starting with "/") are treated as text.
func splitKV(tok string) (string, string, bool) {
	i := strings.IndexByte(tok, ':')
	if i <= 0 {
		return "", "", false
	}
	key, val := tok[:i], tok[i+1:]
	// Require a non-empty value so a prose word ending in a colon ("spike:",
	// "note:") stays text, and skip URL-ish "//…" values.
	if val == "" || !keyRe.MatchString(key) || strings.HasPrefix(val, "/") {
		return "", "", false
	}
	return key, val, true
}

// parseLine parses a single line into a Task. The boolean is false for blank or
// whitespace-only lines, which the caller preserves as raw nodes.
func parseLine(raw string) (*Task, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	toks := strings.Fields(raw)
	t := &Task{raw: raw}
	i := 0
	if toks[i] == "x" {
		t.Done = true
		i++
		if i < len(toks) && isDate(toks[i]) {
			t.Completed = toks[i]
			i++
		}
	}
	if i < len(toks) && isPriority(toks[i]) {
		t.Priority = toks[i][1]
		i++
	}
	if i < len(toks) && isDate(toks[i]) {
		t.Created = toks[i]
		i++
	}
	var text []string
	for ; i < len(toks); i++ {
		if k, v, ok := splitKV(toks[i]); ok {
			t.kvs = append(t.kvs, kv{k, v})
		} else {
			text = append(text, toks[i])
		}
	}
	t.Text = strings.Join(text, " ")
	return t, true
}

// Line returns the on-disk representation: the original raw line when
// unmodified, or a freshly rendered canonical line when mutated.
func (t *Task) Line() string {
	if !t.dirty {
		return t.raw
	}
	return t.render()
}

// render produces the canonical line for a mutated task: status, dates,
// priority, text, then key:value tokens (unknown ones preserved in order, id
// always last).
func (t *Task) render() string {
	var parts []string
	switch {
	case t.Done:
		parts = append(parts, "x")
		if t.Completed != "" {
			parts = append(parts, t.Completed)
		}
	case t.Priority != 0:
		parts = append(parts, "("+string(t.Priority)+")")
	}
	if t.Created != "" {
		parts = append(parts, t.Created)
	}
	if t.Text != "" {
		parts = append(parts, t.Text)
	}
	var id string
	for _, p := range t.kvs {
		if p.Key == "id" {
			id = p.Val
			continue
		}
		parts = append(parts, p.Key+":"+p.Val)
	}
	if id != "" {
		parts = append(parts, "id:"+id)
	}
	return strings.Join(parts, " ")
}

// --- key:value accessors -------------------------------------------------

func (t *Task) get(key string) (string, bool) {
	for _, p := range t.kvs {
		if p.Key == key {
			return p.Val, true
		}
	}
	return "", false
}

func (t *Task) set(key, val string) {
	for i := range t.kvs {
		if t.kvs[i].Key == key {
			t.kvs[i].Val = val
			t.dirty = true
			return
		}
	}
	t.kvs = append(t.kvs, kv{key, val})
	t.dirty = true
}

func (t *Task) del(key string) {
	out := t.kvs[:0]
	for _, p := range t.kvs {
		if p.Key == key {
			t.dirty = true
			continue
		}
		out = append(out, p)
	}
	t.kvs = out
}

// ID returns the task's ULID (empty if none yet).
func (t *Task) ID() string     { v, _ := t.get("id"); return v }
func (t *Task) Due() string    { v, _ := t.get("due"); return v }
func (t *Task) Source() string { v, _ := t.get("src"); return v }
func (t *Task) State() string  { v, _ := t.get("s"); return v }
func (t *Task) Parent() string { v, _ := t.get("parent"); return v }
func (t *Task) Recur() string  { v, _ := t.get("rec"); return v }
func (t *Task) Blocks() string { v, _ := t.get("blocks"); return v }

// Start is the threshold/defer date (todo.txt "t:" key): the task is not
// actionable until this date. Agenda/ready views hide future-start tasks.
func (t *Task) Start() string { v, _ := t.get("t"); return v }

// Key returns the value of an arbitrary key:value token (empty if absent), so
// callers outside the package can read/normalize tokens nt doesn't model.
func (t *Task) Key(name string) string { v, _ := t.get(name); return v }

// Discovered is the ULID of the task this one was discovered while working on —
// provenance for work an agent surfaced mid-task (key: discovered:<ULID>).
func (t *Task) Discovered() string { v, _ := t.get("discovered"); return v }

// EnsureID assigns a ULID if the task lacks one (SPEC §4: hand-added lines get
// an id on the next mutation that touches them).
func (t *Task) EnsureID() {
	if t.ID() == "" {
		t.set("id", ulid.New())
	}
}

// Projects, Tags, and Links are derived from the description text.
func (t *Task) Projects() []string { return prefixed(t.Text, '+') }
func (t *Task) Tags() []string     { return prefixed(t.Text, '@') }

func (t *Task) Links() []string {
	var out []string
	for _, m := range linkRe.FindAllStringSubmatch(t.Text, -1) {
		out = append(out, m[1])
	}
	return out
}

func prefixed(text string, p byte) []string {
	var out []string
	for _, w := range strings.Fields(text) {
		if len(w) > 1 && w[0] == p {
			out = append(out, w[1:])
		}
	}
	return out
}

// --- mutators (all mark the task dirty) ----------------------------------

// SetDone toggles completion. Marking done preserves any (A) priority as a
// pri:A key (SPEC §4); reopening restores it.
func (t *Task) SetDone(done bool, today string) {
	t.dirty = true
	if done {
		if t.Priority != 0 {
			t.set("pri", string(t.Priority))
			t.Priority = 0
		}
		t.Done = true
		t.Completed = today
		t.del("s")
	} else {
		t.Done = false
		t.Completed = ""
		if v, ok := t.get("pri"); ok && len(v) == 1 {
			t.Priority = v[0]
			t.del("pri")
		}
	}
}

// SetPriority sets (0 clears) the priority. No-op on done tasks beyond storing
// the pri key.
func (t *Task) SetPriority(p byte) {
	t.dirty = true
	if t.Done {
		if p == 0 {
			t.del("pri")
		} else {
			t.set("pri", string(p))
		}
		return
	}
	t.Priority = p
}

// SetState sets s:doing / s:blocked, or clears it for "open". "done"/"" are
// handled via SetDone by the caller.
func (t *Task) SetState(s string) {
	if s == "" || s == "open" {
		t.del("s")
		return
	}
	t.set("s", s)
}

// SetKey sets or (empty val) deletes an arbitrary key:value, e.g. due, parent.
func (t *Task) SetKey(key, val string) {
	if val == "" {
		t.del(key)
		return
	}
	t.set(key, val)
}

// SetText replaces the description (used by rename).
func (t *Task) SetText(s string) {
	t.Text = s
	t.dirty = true
}

// AddLink appends a [[target]] link to the description if not already present.
func (t *Task) AddLink(target string) {
	for _, l := range t.Links() {
		if l == target {
			return
		}
	}
	if t.Text != "" {
		t.Text += " "
	}
	t.Text += "[[" + target + "]]"
	t.dirty = true
}

// Status returns a normalized status string for filtering/display.
func (t *Task) Status() string {
	if t.Done {
		return "done"
	}
	if s := t.State(); s != "" {
		return s
	}
	return "open"
}

// ParseLine parses a single line into a Task (raw, unmodified). Used by the
// undo engine to restore a before-image so it renders byte-identically.
func ParseLine(line string) (*Task, bool) {
	return parseLine(line)
}

// New builds a fresh task with a ULID. Callers add text/metadata then it is
// appended to a Doc.
func New(text string) *Task {
	t := &Task{dirty: true, Text: text}
	t.set("id", ulid.New())
	return t
}
