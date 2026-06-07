// Package links implements the unified cross-item linking model (SPEC §5.1):
// a single [[target]] syntax that resolves to a note (by slug, title, or id) or
// a task (by ULID / short prefix), plus backlinks computed on demand by
// scanning files — no stored index.
package links

import (
	"path/filepath"
	"regexp"
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

// Resolve maps a [[target]] to an item: note by path-suffix → note title → note
// id → task ULID. Obsidian resolves links by filename and disambiguates by the
// shortest path, so `[[auth]]` matches any note named auth.md while
// `[[work/auth]]` matches only one under work/. A bare name that matches several
// notes resolves to an Item{Kind:"ambiguous"} with ok=false (callers surface it
// for the user to qualify) rather than silently guessing.
func Resolve(target string, doc *task.Doc, notes []*note.Note) (Item, bool) {
	key, _ := NormalizeTarget(target)
	if key == "" {
		return Item{}, false
	}
	var hits []*note.Note
	for _, n := range notes {
		if suffixMatch(relOf(n), key) {
			hits = append(hits, n)
		}
	}
	switch len(hits) {
	case 1:
		return noteItem(hits[0]), true
	case 0:
		// fall through to title / id / task
	default:
		names := make([]string, len(hits))
		for i, n := range hits {
			names[i] = relOf(n)
		}
		return Item{Kind: "ambiguous", Title: strings.Join(names, ", ")}, false
	}
	for _, n := range notes {
		if strings.EqualFold(n.Title, key) {
			return noteItem(n), true
		}
	}
	// id: the full ULID, or the short code (its 6-char suffix) that `nt note` and
	// `nt list` print — so an agent can reuse the handle it was just given.
	if len(key) >= 4 {
		ku := strings.ToUpper(key)
		var idHits []*note.Note
		for _, n := range notes {
			if n.ID != "" && strings.HasSuffix(strings.ToUpper(n.ID), ku) {
				idHits = append(idHits, n)
			}
		}
		if len(idHits) == 1 {
			return noteItem(idHits[0]), true
		}
	}
	if doc != nil {
		if tk, amb := doc.Resolve(key); tk != nil && !amb {
			return Item{Kind: "task", ID: tk.ID(), Title: tk.Text}, true
		}
	}
	return Item{}, false
}

// NormalizeTarget reduces a raw [[…]] inner string to a resolution key and the
// optional display alias: it strips a |alias, a #heading / #^block fragment, and
// a trailing .md, but KEEPS any folder/ prefix so suffix matching can use it.
func NormalizeTarget(raw string) (key, alias string) {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '|'); i >= 0 {
		alias = strings.TrimSpace(s[i+1:])
		s = s[:i]
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i] // drop #heading and #^block
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".md")
	return strings.Trim(s, "/"), alias
}

// SuffixMatch reports whether targetKey matches noteRel by shortest path-suffix
// — exported for adapters that list candidates for an ambiguous link (the web
// viewer's "did you mean" page) without duplicating the resolution rule.
func SuffixMatch(noteRel, targetKey string) bool { return suffixMatch(noteRel, targetKey) }

// relOf is a note's path relative to notes/ (slash-separated); falls back to the
// basename when Rel wasn't set (e.g. a note loaded outside List).
func relOf(n *note.Note) string {
	if n.Rel != "" {
		return n.Rel
	}
	return filepath.Base(n.Path)
}

// suffixMatch reports whether targetKey's path segments are a tail of the note's
// path segments (case-insensitive), both with .md stripped — Obsidian's
// shortest-path resolution.
func suffixMatch(noteRel, targetKey string) bool {
	ns, ts := segs(noteRel), segs(targetKey)
	if len(ts) == 0 || len(ts) > len(ns) {
		return false
	}
	for i := 1; i <= len(ts); i++ {
		if ns[len(ns)-i] != ts[len(ts)-i] {
			return false
		}
	}
	return true
}

func segs(p string) []string {
	p = strings.Trim(strings.TrimSuffix(strings.ToLower(p), ".md"), "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

var linkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Wikilinks returns the raw inner strings of every [[…]] in s (exported for
// adapters that need a note's outbound links, e.g. the MCP nt_links tool).
func Wikilinks(s string) []string { return wikilinks(s) }

// wikilinks returns the raw inner strings of every [[…]] in s.
func wikilinks(s string) []string {
	var out []string
	for _, m := range linkRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// RewriteLine rewrites every [[…]] in s that resolves (by path-suffix) to the
// note at oldRel so its basename becomes newBase, preserving any folder prefix,
// #fragment, and |alias. Returns the new string and whether anything changed.
func RewriteLine(s, oldRel, newBase string) (string, bool) {
	changed := false
	out := linkRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[2 : len(m)-2]
		if key, _ := NormalizeTarget(inner); key == "" || !suffixMatch(oldRel, key) {
			return m
		}
		changed = true
		path, frag, alias := splitInner(inner)
		seg := strings.Split(path, "/")
		seg[len(seg)-1] = newBase
		return "[[" + strings.Join(seg, "/") + frag + alias + "]]"
	})
	return out, changed
}

// splitInner separates a [[…]] inner string into its path (folder/name, .md and
// surrounding slashes trimmed), its #fragment, and its |alias (each kept with its
// leading delimiter, or empty).
func splitInner(inner string) (path, frag, alias string) {
	s := inner
	if i := strings.IndexByte(s, '|'); i >= 0 {
		alias, s = s[i:], s[:i]
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		frag, s = s[i:], s[:i]
	}
	return strings.Trim(strings.TrimSuffix(strings.TrimSpace(s), ".md"), "/"), frag, alias
}

func noteItem(n *note.Note) Item {
	return Item{Kind: "note", ID: n.ID, Title: n.Title, Path: n.Path}
}

// Backlinks finds lines across tasks.txt and notes/ that link TO the item with
// the given id and rel (rel = a note's path relative to notes/, empty for
// tasks). It excludes the item's own defining line. A coarse ripgrep pass is
// refined in Go so `id:` self-definitions and substring false-positives are
// filtered out, and so a [[work/auth]] / [[auth|alias]] link to this note counts.
func Backlinks(s *store.Store, id, rel string) []search.Hit {
	seen := map[string]bool{}
	var out []search.Hit
	add := func(hits []search.Hit) {
		for _, h := range hits {
			key := h.Path + ":" + itoa(h.Line)
			if seen[key] || !references(h.Text, id, rel) {
				continue
			}
			seen[key] = true
			out = append(out, h)
		}
	}
	if id != "" {
		h, _ := search.Literal(id, s.TasksFile(), s.NotesDir())
		add(h)
	}
	if rel != "" {
		// Any wikilink is a candidate; references() does the precise suffix check.
		h, _ := search.Literal("[[", s.TasksFile(), s.NotesDir())
		add(h)
	}
	return out
}

// references reports whether a line links to the target (not merely defines it).
func references(line, id, rel string) bool {
	if id != "" {
		if strings.Contains(line, "[["+id+"]]") ||
			strings.Contains(line, "parent:"+id) ||
			strings.Contains(line, "blocks:"+id) {
			return true
		}
	}
	if rel != "" {
		for _, raw := range wikilinks(line) {
			if key, _ := NormalizeTarget(raw); key != "" && suffixMatch(rel, key) {
				return true
			}
		}
	}
	return false
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
