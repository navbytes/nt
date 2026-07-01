// Package note implements nt's markdown notes with light YAML frontmatter
// (SPEC §5). Notes are one file each under notes/, so they need no shared lock:
// creation and edits are atomic single-file writes.
package note

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/ulid"
)

// Note is a parsed markdown note.
type Note struct {
	Path     string
	Rel      string // path relative to notes/ (slash-separated), set by List
	ID       string
	Title    string
	Tags     []string
	Aliases  []string
	Source   string
	Created  string
	Updated  string // stamped when nt rewrites the note (retag, --field)
	Archived bool   // frontmatter archived: true — retired from active views, still on disk
	Favorite bool   // frontmatter favorite: true — starred/pinned for quick access
	Body     string
	Extra    []string // raw frontmatter lines for keys nt doesn't model (preserved verbatim)
}

// Slug derives a filesystem-safe slug from a title, falling back to a timestamp
// when the title yields nothing usable (à la nb).
func Slug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = time.Now().Format("2006-01-02-150405")
	}
	return slug
}

// TaskNoteFolder is the subfolder under notes/ where a task's "body" notes live
// (auto-split paragraph captures and explicit task detail). The double-underscore
// name is deliberately "reserved-looking" so it won't collide with a plain
// "tasks" folder a user might keep for their own hand-curated notes; grouping
// these machine-created notes here keeps them out of a human's folders — like the
// "journal" folder does for daily notes.
const TaskNoteFolder = "__tasks__"

// Create builds and writes a new note, returning it. The body is prefixed with
// an H1 title when it doesn't already start with one.
// Create writes a new note. folder, when non-empty, is a slash-separated
// subfolder under notes/ (e.g. "work" or "work/auth"); it is created as needed.
// The filename is slugged from the title; the body and frontmatter are written
// by Save.
func Create(s *store.Store, title, body string, tags []string, source, folder string) (*Note, error) {
	n := &Note{
		ID:      ulid.New(),
		Title:   title,
		Tags:    tags,
		Source:  source,
		Created: time.Now().Format(time.RFC3339),
		Body:    body,
	}
	notesDir := s.NotesDir()
	dir := notesDir
	if clean, err := cleanFolder(folder); err != nil {
		return nil, err
	} else if clean != "" {
		dir = filepath.Join(dir, filepath.FromSlash(clean))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create folder: %w", err)
		}
	}
	p, err := claimPath(dir, Slug(title))
	if err != nil {
		return nil, err
	}
	n.Path = p
	// Defense in depth against path traversal: cleanFolder rejects ".."/absolute
	// folders and Slug strips the title to [a-z0-9-], so the path can't escape
	// notes/ — but with a web endpoint feeding untrusted input we assert it
	// explicitly rather than trust those barriers transitively.
	if !withinDir(notesDir, n.Path) {
		_ = os.Remove(n.Path)
		return nil, fmt.Errorf("refusing to write note outside notes/: %q", n.Path)
	}
	if err := n.Save(); err != nil {
		return nil, err
	}
	return n, nil
}

// withinDir reports whether target resolves inside base (no "../" escape).
func withinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// cleanFolder normalizes a slash-separated subfolder and refuses paths that
// would escape notes/ (absolute, or containing "." / ".." segments).
func cleanFolder(folder string) (string, error) {
	if filepath.IsAbs(folder) {
		return "", fmt.Errorf("folder must be relative to notes/: %q", folder)
	}
	f := strings.Trim(filepath.ToSlash(strings.TrimSpace(folder)), "/")
	if f == "" {
		return "", nil
	}
	for _, seg := range strings.Split(f, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid folder %q", folder)
		}
	}
	return f, nil
}

// claimPath atomically reserves a free note path for a slug. It O_EXCL-creates an
// empty placeholder so two concurrent processes can never pick — and then clobber —
// the same filename (a stat-then-write race that silently loses one write). Save
// later rewrites the placeholder we now own. On a collision it advances the "-N"
// suffix, exactly like the old uniquePath.
func claimPath(dir, slug string) (string, error) {
	for i := 1; i < 1_000_000; i++ {
		name := slug + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.md", slug, i)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return p, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("claim note path: %w", err)
		}
		// exists — another note (or a racing writer) has this slug; try the next.
	}
	return "", fmt.Errorf("too many slug collisions for %q", slug)
}

// Save writes the note atomically with frontmatter.
func (n *Note) Save() error {
	var b strings.Builder
	b.WriteString("---\n")
	if n.ID != "" {
		fmt.Fprintf(&b, "id: %s\n", n.ID)
	}
	if len(n.Tags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(n.Tags, ", "))
	}
	if len(n.Aliases) > 0 {
		fmt.Fprintf(&b, "aliases: [%s]\n", strings.Join(n.Aliases, ", "))
	}
	// Persist the title when the body's own H1 would otherwise win on reload
	// (Load's precedence is frontmatter title → alias → first body heading). Without
	// this, `nt note --title X` with a body starting "# Y" silently becomes titled
	// "Y" — breaking the index and get-by-title. Only emitted when it actually
	// differs, so Obsidian notes whose H1 == title stay frontmatter-clean.
	if n.Title != "" {
		if bh := firstHeading(n.Body); bh != "" && bh != n.Title {
			fmt.Fprintf(&b, "title: %s\n", n.Title)
		}
	}
	if n.Source != "" {
		fmt.Fprintf(&b, "source: %s\n", n.Source)
	}
	if n.Created != "" {
		fmt.Fprintf(&b, "created: %s\n", n.Created)
	}
	if n.Updated != "" {
		fmt.Fprintf(&b, "updated: %s\n", n.Updated)
	}
	if n.Archived {
		b.WriteString("archived: true\n")
	}
	if n.Favorite {
		b.WriteString("favorite: true\n")
	}
	for _, line := range n.Extra { // unknown keys (Obsidian properties), verbatim
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("---\n\n")
	body := n.Body
	if n.Title != "" && !strings.HasPrefix(strings.TrimSpace(body), "#") {
		fmt.Fprintf(&b, "# %s\n\n", n.Title)
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	return store.WriteAtomic(n.Path, []byte(b.String()), 0o644)
}

var fmDelim = "---"

// Load parses a note file (frontmatter + body). Unknown frontmatter keys are
// ignored, not an error.
func Load(path string) (*Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := &Note{Path: path}
	text := string(data)
	if strings.HasPrefix(text, fmDelim+"\n") {
		rest := text[len(fmDelim)+1:]
		if end := strings.Index(rest, "\n"+fmDelim); end >= 0 {
			parseFrontmatter(rest[:end], n)
			body := rest[end+len(fmDelim)+1:]
			n.Body = strings.TrimPrefix(body, "\n")
		}
	} else {
		n.Body = text
	}
	// Title precedence: frontmatter title (set during parse) → first alias →
	// first H1 → humanized filename. Covers Obsidian notes that have no H1.
	if n.Title == "" && len(n.Aliases) > 0 {
		n.Title = n.Aliases[0]
	}
	if n.Title == "" {
		n.Title = firstHeading(n.Body)
	}
	if n.Title == "" {
		n.Title = humanizeFilename(path)
	}
	return n, nil
}

var listRe = regexp.MustCompile(`\[(.*)\]`)

// parseFrontmatter reads the keys nt understands from a YAML-ish frontmatter
// block. Beyond nt's own output it tolerates Obsidian conventions: block-list
// and bare-comma tags/aliases, a title:/aliases: key, and the deprecated
// singular tag:. Unknown keys are ignored.
func parseFrontmatter(fm string, n *Note) {
	lines := strings.Split(fm, "\n")
	for i := 0; i < len(lines); i++ {
		ci := strings.IndexByte(lines[i], ':')
		if ci < 0 {
			continue
		}
		key := strings.TrimSpace(lines[i][:ci])
		val := strings.TrimSpace(lines[i][ci+1:])
		switch key {
		case "id":
			n.ID = unquote(val)
		case "source":
			n.Source = unquote(val)
		case "created":
			n.Created = unquote(val)
		case "updated":
			n.Updated = unquote(val)
		case "archived":
			n.Archived = unquote(val) == "true"
		case "favorite":
			n.Favorite = unquote(val) == "true"
		case "title":
			if v := unquote(val); v != "" {
				n.Title = v
			}
		case "tag": // deprecated singular form
			n.Tags = appendClean(n.Tags, val)
		case "tags":
			n.Tags = append(n.Tags, parseList(val, lines, &i)...)
		case "alias", "aliases":
			n.Aliases = append(n.Aliases, parseList(val, lines, &i)...)
		default:
			// Unknown key (e.g. an Obsidian property): preserve it verbatim,
			// including any block-list continuation lines, so a later rewrite
			// (retag, --field, updated stamp) never clobbers it.
			n.Extra = append(n.Extra, lines[i])
			if val == "" {
				for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
					n.Extra = append(n.Extra, lines[i+1])
					i++
				}
			}
		}
	}
}

// parseList reads a YAML list value in any of the forms Obsidian/nt emit: inline
// flow `[a, b]`, bare comma `a, b`, or a block list of following `- item` lines
// (consuming them, advancing *i).
func parseList(val string, lines []string, i *int) []string {
	var out []string
	switch {
	case strings.HasPrefix(val, "["):
		if m := listRe.FindStringSubmatch(val); m != nil {
			for _, t := range strings.Split(m[1], ",") {
				out = appendClean(out, t)
			}
		}
	case val != "":
		for _, t := range strings.Split(val, ",") {
			out = appendClean(out, t)
		}
	default: // block list: indented "- item" lines on following rows
		for *i+1 < len(lines) {
			t := strings.TrimSpace(lines[*i+1])
			if !strings.HasPrefix(t, "- ") {
				break
			}
			out = appendClean(out, t[2:])
			*i++
		}
	}
	return out
}

// appendClean trims quotes/whitespace and a stray leading '#', dropping empties.
func appendClean(out []string, s string) []string {
	s = strings.TrimPrefix(unquote(strings.TrimSpace(s)), "#")
	if s = strings.TrimSpace(s); s != "" {
		out = append(out, s)
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

// humanizeFilename turns a note filename into a readable title fallback.
func humanizeFilename(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

// List loads all notes in the store's notes directory, recursing into
// subfolders so an Obsidian-style nested vault works. Hidden dirs (.obsidian/,
// .trash/, .git/) and non-.md files are skipped. Each note's Rel (path relative
// to notes/, slash-separated) is set for link resolution; results are sorted by
// Rel for deterministic ordering.
// Active drops archived notes — the working set, for views/search that should
// hide retired notes. List itself returns everything (archived included) so
// link-rewriting and the archived view still see them.
// Description returns the note's one-line summary for index/stub views: its
// `description:` frontmatter if set (kept in Extra, since nt doesn't model the
// key), else the first non-heading body line. Clamped to a single line ≤max chars.
// This is the "one-sentence summary" granularity of progressive disclosure — what
// an agent reads to decide whether to open the full note.
func (n *Note) Description(max int) string {
	for _, line := range n.Extra {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "description") {
			if d := strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`)); d != "" {
				return clampLine(d, max)
			}
		}
	}
	for _, raw := range strings.Split(n.Body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return clampLine(line, max)
	}
	return ""
}

func clampLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace to one line
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max-1]) + "…"
	}
	return s
}

// Reserved reports whether a note lives in a machine-managed folder that isn't
// part of the human/agent knowledge base — currently notes/__tasks__/, where
// nt files the detail bodies of split tasks. These are reachable by id/link but
// are kept out of the KB catalog (nt index) and search so they don't pollute it.
func (n *Note) Reserved() bool { return strings.HasPrefix(n.Rel, "__tasks__/") }

func Active(ns []*Note) []*Note {
	out := ns[:0:0]
	for _, n := range ns {
		if !n.Archived {
			out = append(out, n)
		}
	}
	return out
}

func List(s *store.Store) ([]*Note, error) {
	dir := s.NotesDir()
	var out []*Note
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries rather than aborting the walk
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		n, e := Load(path)
		if e != nil {
			return nil
		}
		if rel, e := filepath.Rel(dir, path); e == nil {
			n.Rel = filepath.ToSlash(rel)
		}
		out = append(out, n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}
