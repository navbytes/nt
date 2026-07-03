package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ageNote rewrites a note's frontmatter dates and mtime so it qualifies for gc.
func ageNote(t *testing.T, dir, rel string, days int) {
	t.Helper()
	p := filepath.Join(dir, "notes", rel)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -days)
	iso := old.Format("2006-01-02T15:04:05Z")
	s := string(b)
	for _, k := range []string{"created: ", "updated: "} {
		if i := strings.Index(s, k); i >= 0 {
			j := strings.IndexByte(s[i:], '\n')
			s = s[:i] + k + iso + s[i+j:]
		}
	}
	os.WriteFile(p, []byte(s), 0o644)
	os.Chtimes(p, old, old)
}

func TestGcCollectsSupersededAndStranded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)

	// A superseded stub…
	captureRun(t, "note", "token storage v1", "--description", "x")
	captureRun(t, "note", "token storage v2", "--description", "y", "--supersede", "token storage v1")
	// …a stranded task-detail note (task removed, note left behind)…
	captureRun(t, "add", "doomed task", "--body", "detail that will be stranded")
	var listed []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(captureRun(t, "list", "--json")), &listed)
	captureRun(t, "rm", listed[0].ID, "-y")
	// …a live task with detail (must NOT be collected), and a live superseding note.
	captureRun(t, "add", "living task", "--body", "detail that must survive")

	// Nothing is old enough yet: default 30d retention keeps everything.
	out := captureRun(t, "gc")
	if !strings.Contains(out, "nothing to gc") {
		t.Fatalf("fresh notes must not be collected:\n%s", out)
	}

	// Age the dead weight past the cutoff.
	ageNote(t, dir, "token-storage-v1.md", 40)
	ageNote(t, dir, "__tasks__/doomed-task.md", 40)

	out = captureRun(t, "gc")
	if !strings.Contains(out, "superseded") || !strings.Contains(out, "stranded") || !strings.Contains(out, "dry-run") {
		t.Fatalf("gc plan should list both classes and say dry-run:\n%s", out)
	}
	if strings.Contains(out, "living task") || strings.Contains(out, "token storage v2") {
		t.Fatalf("gc must not touch live notes:\n%s", out)
	}
	// Dry run moved nothing.
	if _, err := os.Stat(filepath.Join(dir, "notes", "token-storage-v1.md")); err != nil {
		t.Fatal("dry-run must not move files")
	}

	captureRun(t, "gc", "--yes")
	if _, err := os.Stat(filepath.Join(dir, "notes", "token-storage-v1.md")); !os.IsNotExist(err) {
		t.Fatal("superseded stub should be trashed")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "__tasks__", "doomed-task.md")); !os.IsNotExist(err) {
		t.Fatal("stranded detail note should be trashed")
	}
	// Recoverable: both live in .trash/.
	trash, _ := os.ReadDir(filepath.Join(dir, ".trash"))
	if len(trash) != 2 {
		t.Fatalf(".trash should hold the 2 collected notes, got %d", len(trash))
	}
	// The live task's detail note survives.
	if _, err := os.Stat(filepath.Join(dir, "notes", "__tasks__", "living-task.md")); err != nil {
		t.Fatal("live detail note must survive gc")
	}
	// And now there's nothing left.
	out = captureRun(t, "gc")
	if !strings.Contains(out, "nothing to gc") {
		t.Fatalf("second gc should be clean:\n%s", out)
	}
}
