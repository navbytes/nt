package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: a prose title ending with a path ("… valid at .claude/…/x/")
// used to be split at its last slash into folder + EMPTY title and rejected
// with "nt: note: a title is required". It must create normally, filed by
// --kind, with the title intact.
func TestCmdNoteProseTrailingSlashTitle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	title := "Search (2.0) SHELVED pre-build — design valid at .claude/company/release-2.0-search/"
	out := captureRun(t, "note", title, "--kind", "decision")
	if !strings.Contains(out, "notes/decisions/search-2-0-shelved-pre-build") {
		t.Fatalf("prose title mis-split, got: %q", out)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "notes", "decisions", "search-2-0-shelved-pre-build*.md"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 note under decisions/, got %v", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# "+title) {
		t.Fatalf("title not intact in note:\n%s", b)
	}
}

// A slash deep in prose (whitespace anywhere before it) is part of the title,
// not a filing choice — the note lands at the notes/ root, title whole.
func TestCmdNoteInteriorProseSlashTitle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	out := captureRun(t, "note", "Smoke tests must poll apps/web after every deploy")
	if !strings.Contains(out, "notes/smoke-tests-must-poll-apps-web-after-every-deploy.md") {
		t.Fatalf("interior prose slash mis-split, got: %q", out)
	}
}
