package links

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
)

func mkNote(rel, id, title string) *note.Note {
	return &note.Note{Rel: rel, ID: id, Title: title, Path: "/x/notes/" + rel}
}

func TestNormalizeTarget(t *testing.T) {
	cases := []struct{ in, key, alias string }{
		{"note", "note", ""},
		{"note|Alias", "note", "Alias"},
		{"note#heading", "note", ""},
		{"note#^block", "note", ""},
		{"folder/note", "folder/note", ""},
		{"note.md", "note", ""},
		{"folder/note.md|Disp", "folder/note", "Disp"},
	}
	for _, c := range cases {
		if k, a := NormalizeTarget(c.in); k != c.key || a != c.alias {
			t.Errorf("NormalizeTarget(%q) = (%q,%q), want (%q,%q)", c.in, k, a, c.key, c.alias)
		}
	}
}

func TestResolveSuffixAndAmbiguity(t *testing.T) {
	notes := []*note.Note{mkNote("auth.md", "01A", "Auth"), mkNote("work/spec.md", "02B", "Spec")}
	if it, ok := Resolve("auth", nil, notes); !ok || it.ID != "01A" {
		t.Fatalf("bare resolve: %+v ok=%v", it, ok)
	}
	if it, ok := Resolve("Auth#Section|Display", nil, notes); !ok || it.ID != "01A" {
		t.Fatalf("variant + case-insensitive: %+v ok=%v", it, ok)
	}
	if it, ok := Resolve("work/spec", nil, notes); !ok || it.ID != "02B" {
		t.Fatalf("folder-qualified: %+v ok=%v", it, ok)
	}

	// Collision: bare name is ambiguous; folder qualifier disambiguates.
	coll := []*note.Note{mkNote("work/auth.md", "W", "WAuth"), mkNote("personal/auth.md", "P", "PAuth")}
	if it, ok := Resolve("auth", nil, coll); ok || it.Kind != "ambiguous" {
		t.Fatalf("bare colliding name should be ambiguous: %+v ok=%v", it, ok)
	}
	if it, ok := Resolve("work/auth", nil, coll); !ok || it.ID != "W" {
		t.Fatalf("qualified link should disambiguate: %+v ok=%v", it, ok)
	}
}

func TestReferences(t *testing.T) {
	if !references("see [[work/auth]] today", "", "work/auth.md") {
		t.Error("qualified link should reference work/auth")
	}
	if !references("see [[auth|alias]] today", "", "work/auth.md") {
		t.Error("bare aliased link should reference work/auth")
	}
	if references("see [[personal/auth]]", "", "work/auth.md") {
		t.Error("personal/auth must NOT reference work/auth")
	}
	if !references("blocks:01A", "01A", "") {
		t.Error("blocks: should count as a reference")
	}
}

func TestRewriteLine(t *testing.T) {
	// All forms that resolve to work/old.md get their basename swapped, keeping
	// the folder prefix, #fragment, and |alias.
	in := "a [[old]] b [[work/old#sec|Plan]] c [[other/old]] d [[unrelated]]"
	out, changed := RewriteLine(in, "work/old.md", "new")
	if !changed {
		t.Fatal("expected a rewrite")
	}
	want := "a [[new]] b [[work/new#sec|Plan]] c [[other/old]] d [[unrelated]]"
	if out != want {
		t.Fatalf("RewriteLine:\n got %q\nwant %q", out, want)
	}
	if _, ch := RewriteLine("no links here", "work/old.md", "new"); ch {
		t.Error("should report no change when nothing matches")
	}
}

func TestBacklinksIntegration(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(s.NotesDir(), "work"), 0o755)
	_ = os.WriteFile(filepath.Join(s.NotesDir(), "work", "auth.md"), []byte("# Auth\n"), 0o644)
	_ = os.WriteFile(filepath.Join(s.NotesDir(), "refs.md"), []byte("see [[work/auth]] and [[auth|x]]\n"), 0o644)

	if back := Backlinks(s, "", "work/auth.md"); len(back) == 0 {
		t.Fatal("expected a backlink to work/auth")
	}
}
