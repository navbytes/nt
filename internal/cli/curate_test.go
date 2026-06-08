package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdTagPreservesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "notes", "x.md")
	if err := os.WriteFile(p, []byte("---\nid: 01ABC\ntags: [a]\nstatus: draft\ncssclass: wide\n---\n# X\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := runWithStdout("tag", "x", "+b", "-a"); code != 0 {
		t.Fatalf("nt tag exit %d: %s", code, out)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	if !strings.Contains(s, "tags: [b]") {
		t.Fatalf("retag failed:\n%s", s)
	}
	if !strings.Contains(s, "status: draft") || !strings.Contains(s, "cssclass: wide") {
		t.Fatalf("retag clobbered Obsidian frontmatter:\n%s", s)
	}
}

func TestCmdSearchTagFilter(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Alpha", "--tag", "x")
	captureRun(t, "note", "Beta", "--tag", "y")
	out := captureRun(t, "search", "--tag", "x", "--type", "note")
	if !strings.Contains(out, "Alpha") || strings.Contains(out, "Beta") {
		t.Fatalf("--tag filter wrong:\n%s", out)
	}
}

func TestCmdTagsVocabulary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "A", "--tag", "auth", "--tag", "ref")
	captureRun(t, "add", "do it", "--tag", "auth")
	out := captureRun(t, "tags")
	if !strings.Contains(out, "@auth") || !strings.Contains(out, "@ref") {
		t.Fatalf("tags listing missing entries:\n%s", out)
	}
}

func TestCmdRmNoteDangling(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Target")
	captureRun(t, "note", "Source", "--body", "see [[target]]")
	if _, code := runWithStdout("rm", "target"); code == 0 {
		t.Fatal("rm should refuse a note with inbound links")
	}
	if _, code := runWithStdout("rm", "target", "--force"); code != 0 {
		t.Fatal("rm --force should delete the note")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "target.md")); err == nil {
		t.Fatal("note should have been moved out of notes/")
	}
}

func TestCmdNoteField(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Spec", "--field", "status=stable")
	b, _ := os.ReadFile(filepath.Join(dir, "notes", "spec.md"))
	if !strings.Contains(string(b), "status: stable") {
		t.Fatalf("--field not written:\n%s", b)
	}
}

func TestCmdTagStampsUpdated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "X", "--tag", "a")
	captureRun(t, "tag", "x", "+b")
	b, _ := os.ReadFile(filepath.Join(dir, "notes", "x.md"))
	if !strings.Contains(string(b), "updated:") {
		t.Fatalf("retag should stamp updated:\n%s", b)
	}
}

func TestCmdLinksOrphans(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Lonely")
	captureRun(t, "note", "Hub", "--body", "see [[lonely]]")
	out := captureRun(t, "links", "--orphans")
	if !strings.Contains(out, "Hub") || strings.Contains(out, "Lonely") {
		t.Fatalf("orphans wrong (Hub is the orphan, Lonely is linked):\n%s", out)
	}
}

func TestCmdTagBulk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Alpha")
	captureRun(t, "note", "Beta")

	if out, code := runWithStdout("tag", "alpha", "beta", "+reviewed"); code != 0 {
		t.Fatalf("bulk tag exit %d: %s", code, out)
	}
	for _, slug := range []string{"alpha", "beta"} {
		got, _ := os.ReadFile(filepath.Join(dir, "notes", slug+".md"))
		if !strings.Contains(string(got), "reviewed") {
			t.Errorf("%s should have @reviewed:\n%s", slug, got)
		}
	}
}
