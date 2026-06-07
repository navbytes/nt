package note

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/navbytes/nt/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("NT_DIR", t.TempDir())
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func write(t *testing.T, s *store.Store, rel, content string) {
	t.Helper()
	p := filepath.Join(s.NotesDir(), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRoundTripNative: an nt-created note re-loads with identical fields (guards
// the hand-rolled parser changes against the native inline-tag format).
func TestRoundTripNative(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "JWT Expiry", "Tokens last 24h.", []string{"auth", "backend"}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "JWT Expiry" || got.Source != "claude" {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "auth" || got.Tags[1] != "backend" {
		t.Fatalf("inline tags should round-trip, got %v", got.Tags)
	}
}

// TestListRecursesAndSkips: List finds notes in subfolders and ignores hidden
// dirs + non-md files.
func TestListRecursesAndSkips(t *testing.T) {
	s := testStore(t)
	write(t, s, "top.md", "# Top\n")
	write(t, s, "work/deep.md", "# Deep\n")
	write(t, s, ".obsidian/app.json", "{}")
	write(t, s, "work/diagram.png", "binary")

	notes, err := List(s)
	if err != nil {
		t.Fatal(err)
	}
	rels := map[string]bool{}
	for _, n := range notes {
		rels[n.Rel] = true
	}
	if !rels["top.md"] || !rels["work/deep.md"] {
		t.Fatalf("List should find top + subfolder notes, got %v", rels)
	}
	if len(notes) != 2 {
		t.Fatalf("List should skip .obsidian/ and .png, got %d notes", len(notes))
	}
}

// TestObsidianFrontmatter: block-list tags, bare-comma tags, singular tag,
// title:, and aliases: are all parsed.
func TestObsidianFrontmatter(t *testing.T) {
	s := testStore(t)
	write(t, s, "block.md", "---\ntags:\n  - auth\n  - backend\naliases:\n  - JWT\n---\nbody\n")
	write(t, s, "comma.md", "---\ntags: auth, backend\n---\nbody\n")
	write(t, s, "single.md", "---\ntag: solo\n---\nbody\n")
	write(t, s, "titled.md", "---\ntitle: Real Title\n---\nno heading here\n")

	load := func(rel string) *Note {
		n, err := Load(filepath.Join(s.NotesDir(), rel))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := load("block.md"); len(n.Tags) != 2 || n.Tags[0] != "auth" || len(n.Aliases) != 1 || n.Aliases[0] != "JWT" {
		t.Fatalf("block list: tags=%v aliases=%v", n.Tags, n.Aliases)
	}
	if n := load("comma.md"); len(n.Tags) != 2 {
		t.Fatalf("bare comma tags: %v", n.Tags)
	}
	if n := load("single.md"); len(n.Tags) != 1 || n.Tags[0] != "solo" {
		t.Fatalf("singular tag: %v", n.Tags)
	}
	if n := load("titled.md"); n.Title != "Real Title" {
		t.Fatalf("frontmatter title should win, got %q", n.Title)
	}
}

// TestTitleFallback: frontmatter title → alias → H1 → humanized filename.
func TestTitleFallback(t *testing.T) {
	s := testStore(t)
	write(t, s, "from-alias.md", "---\naliases:\n  - Aliased Name\n---\nbody\n")
	write(t, s, "from-h1.md", "# Heading Title\n\nbody\n")
	write(t, s, "my-plain-note.md", "just text, no heading\n")

	load := func(rel string) *Note {
		n, _ := Load(filepath.Join(s.NotesDir(), rel))
		return n
	}
	if got := load("from-alias.md").Title; got != "Aliased Name" {
		t.Errorf("alias fallback: %q", got)
	}
	if got := load("from-h1.md").Title; got != "Heading Title" {
		t.Errorf("h1: %q", got)
	}
	if got := load("my-plain-note.md").Title; got != "my plain note" {
		t.Errorf("filename fallback: %q", got)
	}
}
