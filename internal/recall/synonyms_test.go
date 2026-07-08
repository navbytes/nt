package recall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// restoreConceptOf resets the package-level conceptOf table after a test that
// calls LoadUserSynonyms, so later tests see the built-in table again.
func restoreConceptOf(t *testing.T) {
	t.Helper()
	saved := conceptOf
	t.Cleanup(func() { conceptOf = saved })
}

func TestParseSynonymFileGroupsCommentsAndBlankLines(t *testing.T) {
	src := "# a comment\n\nauth, login, signin\nsoloword\ncache caching  memo\n"
	got := parseSynonymFile(src)
	if len(got) != 2 {
		t.Fatalf("want 2 groups (solo line dropped), got %v", got)
	}
	if got[0][0] != "auth" || len(got[0]) != 3 {
		t.Errorf("first group wrong: %v", got[0])
	}
	if got[1][0] != "cache" || len(got[1]) != 3 {
		t.Errorf("second group wrong: %v", got[1])
	}
}

func TestLoadUserSynonymsMissingFileIsNotAnError(t *testing.T) {
	restoreConceptOf(t)
	if err := LoadUserSynonyms(t.TempDir()); err != nil {
		t.Fatalf("a missing synonyms.txt should not error: %v", err)
	}
}

func TestLoadUserSynonymsExtendsAnExistingGroup(t *testing.T) {
	restoreConceptOf(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "synonyms.txt"), []byte("goroutine, houseterm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserSynonyms(dir); err != nil {
		t.Fatal(err)
	}
	if conceptOf[stem("houseterm")] != conceptOf[stem("goroutine")] {
		t.Errorf("houseterm should join goroutine's existing concurrency group")
	}
}

func TestLoadUserSynonymsMintsANewGroup(t *testing.T) {
	restoreConceptOf(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "synonyms.txt"), []byte("widget, gadget, doohickey\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserSynonyms(dir); err != nil {
		t.Fatal(err)
	}
	c := conceptOf[stem("widget")]
	if c == "" {
		t.Fatal("widget should have a concept id")
	}
	if conceptOf[stem("gadget")] != c || conceptOf[stem("doohickey")] != c {
		t.Errorf("widget/gadget/doohickey should share one new concept id")
	}
}

func TestUserSynonymsAffectRanking(t *testing.T) {
	restoreConceptOf(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "synonyms.txt"), []byte("frobnicate, twiddle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserSynonyms(dir); err != nil {
		t.Fatal(err)
	}
	notes := []*note.Note{
		mk("Frobnicate the widget registry", "internal note about frobnication", "lesson"),
	}
	got := Rank(notes, "how do I twiddle the registry", 5)
	if len(got) == 0 {
		t.Fatal("user synonym should let 'twiddle' find the 'frobnicate' note")
	}
}
