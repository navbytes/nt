// Package note implements nt's markdown notes with light YAML frontmatter
// (SPEC §5). Notes are one file each under notes/, so they need no shared lock:
// creation and edits are atomic single-file writes.
package note

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/ulid"
)

// Note is a parsed markdown note.
type Note struct {
	Path    string
	ID      string
	Title   string
	Tags    []string
	Source  string
	Created string
	Body    string
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

// Create builds and writes a new note, returning it. The body is prefixed with
// an H1 title when it doesn't already start with one.
func Create(s *store.Store, title, body string, tags []string, source string) (*Note, error) {
	n := &Note{
		ID:      ulid.New(),
		Title:   title,
		Tags:    tags,
		Source:  source,
		Created: time.Now().Format(time.RFC3339),
		Body:    body,
	}
	slug := Slug(title)
	n.Path = uniquePath(s.NotesDir(), slug)
	if err := n.Save(); err != nil {
		return nil, err
	}
	return n, nil
}

// uniquePath avoids clobbering an existing note with the same slug.
func uniquePath(dir, slug string) string {
	p := filepath.Join(dir, slug+".md")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	for i := 2; ; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s-%d.md", slug, i))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
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
	if n.Source != "" {
		fmt.Fprintf(&b, "source: %s\n", n.Source)
	}
	if n.Created != "" {
		fmt.Fprintf(&b, "created: %s\n", n.Created)
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
	if n.Title == "" {
		n.Title = firstHeading(n.Body)
	}
	return n, nil
}

var listRe = regexp.MustCompile(`\[(.*)\]`)

func parseFrontmatter(fm string, n *Note) {
	for _, line := range strings.Split(fm, "\n") {
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		switch key {
		case "id":
			n.ID = val
		case "source":
			n.Source = val
		case "created":
			n.Created = val
		case "tags":
			if m := listRe.FindStringSubmatch(val); m != nil {
				for _, t := range strings.Split(m[1], ",") {
					if t = strings.TrimSpace(t); t != "" {
						n.Tags = append(n.Tags, t)
					}
				}
			}
		}
	}
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

// List loads all notes in the store's notes directory.
func List(s *store.Store) ([]*Note, error) {
	entries, err := os.ReadDir(s.NotesDir())
	if err != nil {
		return nil, err
	}
	var out []*Note
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		n, err := Load(filepath.Join(s.NotesDir(), e.Name()))
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}
