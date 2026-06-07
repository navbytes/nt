package note

import (
	"os"
	"path/filepath"
	"strings"
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
	n, err := Create(s, "JWT Expiry", "Tokens last 24h.", []string{"auth", "backend"}, "claude", "")
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

// TestCreateInFolder: Create writes into a subfolder, the file lands there, and
// List discovers it with the expected slash-separated Rel.
func TestCreateInFolder(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Auth Design", "scoped", nil, "cli", "work/auth")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.NotesDir(), "work", "auth", "auth-design.md")
	if n.Path != want {
		t.Fatalf("note path = %q, want %q", n.Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("file not created in subfolder: %v", err)
	}
	all, _ := List(s)
	var found bool
	for _, x := range all {
		if x.Rel == "work/auth/auth-design.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("List did not surface the folder note; got %d notes", len(all))
	}
}

// TestCreateFolderEscapeRefused: folders that would escape notes/ are rejected.
func TestCreateFolderEscapeRefused(t *testing.T) {
	s := testStore(t)
	for _, bad := range []string{"../etc", "a/../../b", "/abs/path", "work/./x"} {
		if _, err := Create(s, "x", "", nil, "cli", bad); err == nil {
			t.Errorf("folder %q should be refused", bad)
		}
	}
	// A clean nested folder is allowed.
	if _, err := Create(s, "ok", "", nil, "cli", "a/b/c"); err != nil {
		t.Errorf("clean nested folder should be allowed: %v", err)
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

func TestSavePreservesUnknownFrontmatter(t *testing.T) {
	s := testStore(t)
	raw := "---\nid: 01ABC\ntags: [a, b]\naliases: [Alt Name]\nstatus: stable\ncssclass: wide\nkeywords:\n  - jwt\n  - auth\ncreated: 2026-01-01T00:00:00Z\n---\n\n# Body\n\ncontent\n"
	p := filepath.Join(s.NotesDir(), "x.md")
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	n.Tags = []string{"a", "c"} // simulate a retag, then Save
	if err := n.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	for _, want := range []string{"status: stable", "cssclass: wide", "keywords:", "- jwt", "- auth", "aliases: [Alt Name]", "tags: [a, c]"} {
		if !strings.Contains(got, want) {
			t.Errorf("Save dropped %q from frontmatter:\n%s", want, got)
		}
	}
}
