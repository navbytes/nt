// Package quickadd normalizes the natural-language conventions people type into
// a task's text — across the CLI, TUI, web, and MCP add paths — so they all
// behave identically. It resolves inline date keys (due:, t:) through dateparse
// (so "due:fri" becomes a real date instead of the literal string "fri", the
// bug where the web quick-add silently corrupted task structure) and lifts a
// leading/standalone priority marker ("!high", "!a") onto the task.
//
// It is deliberately conservative: it only acts on explicit, unambiguous tokens
// (a recognized key with a parseable value, or a "!"-prefixed priority word) and
// never guesses a date from free prose, so literal description text is never
// mangled and the todo.txt round-trip stays lossless.
package quickadd

import (
	"strings"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/task"
)

// New builds a task from a raw quick-add string, applying the natural-language
// conventions. Unlike task.New (which keeps the whole string as the description
// for callers that compose text from flags), it parses inline tokens the way a
// todo.txt line is parsed — so "pay rent due:fri @home !high" yields a task with
// a real due date, an @home context, and priority A. The task is assigned an id.
func New(text string) *task.Task {
	t, ok := task.ParseLine(text)
	if !ok { // blank/whitespace — let the caller's validation handle it
		t = task.New("")
	}
	Apply(t)
	t.EnsureID()
	return t
}

// Apply normalizes an already-parsed task in place (inline date keys resolved,
// priority marker lifted). New calls it; surfaces that build a task another way
// can call it directly.
func Apply(t *task.Task) {
	normalizeDateKey(t, "due")
	normalizeDateKey(t, "t")
	liftPriority(t)
}

// normalizeDateKey resolves a date-valued key:value token through dateparse,
// rewriting "due:fri"/"t:tomorrow" to an ISO date. Unparseable values are left
// untouched (they may be an intentional literal or an unknown convention).
func normalizeDateKey(t *task.Task, key string) {
	v := t.Key(key)
	if v == "" {
		return
	}
	if iso, ok := dateparse.Date(v); ok && iso != "" && iso != v {
		t.SetKey(key, iso)
	}
}

// liftPriority pulls a standalone "!<word>" priority marker out of the
// description and applies it, unless the task already has a priority. Recognized
// words are the same ones dateparse.Priority accepts (high/med/low or a/b/c).
func liftPriority(t *task.Task) {
	if t.Priority != 0 {
		return
	}
	fields := strings.Fields(t.Text)
	kept := make([]string, 0, len(fields))
	var found byte
	for _, f := range fields {
		if found == 0 && len(f) > 1 && f[0] == '!' {
			if p, ok := dateparse.Priority(f[1:]); ok && p != 0 {
				found = p
				continue // drop the marker from the text
			}
		}
		kept = append(kept, f)
	}
	if found != 0 {
		t.SetPriority(found)
		t.SetText(strings.Join(kept, " "))
	}
}
