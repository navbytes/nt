// Package links implements the unified cross-item linking model (SPEC §5.1):
// a single [[target]] syntax that resolves to a note (by slug, title, or id) or
// a task (by ULID / short prefix), plus backlinks computed on demand by
// scanning files — no stored index.
package links

import (
	"path/filepath"
	"strings"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// Item is a resolved link target.
type Item struct {
	Kind  string // "task" | "note"
	ID    string
	Title string
	Path  string // notes only
}

// Resolve maps a [[target]] to an item, following the SPEC §5.1 order:
// note slug → note title → note id → task ULID (full or unambiguous prefix).
func Resolve(target string, doc *task.Doc, notes []*note.Note) (Item, bool) {
	t := strings.TrimSpace(target)
	// note slug (filename without .md)
	for _, n := range notes {
		if slugOf(n) == t {
			return noteItem(n), true
		}
	}
	// note title (case-insensitive)
	for _, n := range notes {
		if strings.EqualFold(n.Title, t) {
			return noteItem(n), true
		}
	}
	// note id
	for _, n := range notes {
		if n.ID == t {
			return noteItem(n), true
		}
	}
	// task ULID full or unambiguous short prefix
	if doc != nil {
		if tk, amb := doc.Resolve(t); tk != nil && !amb {
			return Item{Kind: "task", ID: tk.ID(), Title: tk.Text}, true
		}
	}
	return Item{}, false
}

func slugOf(n *note.Note) string {
	return strings.TrimSuffix(filepath.Base(n.Path), ".md")
}

func noteItem(n *note.Note) Item {
	return Item{Kind: "note", ID: n.ID, Title: n.Title, Path: n.Path}
}

// Backlinks finds lines across tasks.txt and notes/ that link TO the item with
// the given id and slug (slug may be empty for tasks). It excludes the item's
// own defining line. Uses ripgrep when available, refining matches in Go so
// `id:` self-definitions and substring false-positives are filtered out.
func Backlinks(s *store.Store, id, slug string) []search.Hit {
	seen := map[string]bool{}
	var out []search.Hit
	add := func(hits []search.Hit) {
		for _, h := range hits {
			key := h.Path + ":" + itoa(h.Line)
			if seen[key] || !references(h.Text, id, slug) {
				continue
			}
			seen[key] = true
			out = append(out, h)
		}
	}
	coarse, _ := search.Literal(id, s.TasksFile(), s.NotesDir())
	add(coarse)
	if slug != "" {
		bySlug, _ := search.Literal("[["+slug+"]]", s.TasksFile(), s.NotesDir())
		add(bySlug)
	}
	return out
}

// references reports whether a line links to the target (not merely defines it).
func references(line, id, slug string) bool {
	if id != "" {
		if strings.Contains(line, "[["+id+"]]") ||
			strings.Contains(line, "parent:"+id) ||
			strings.Contains(line, "blocks:"+id) {
			return true
		}
	}
	return slug != "" && strings.Contains(line, "[["+slug+"]]")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
