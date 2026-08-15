package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The full drift lifecycle: `nt export --out` records what was compiled where;
// `nt doctor` re-renders that selection against the current store and flags a
// target that no longer matches — the previously silent failure mode where a
// pruned rule kept costing tokens (and a new one never arrived) because nobody
// re-ran export.
func TestExportDriftDetectedByDoctor(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Run gofmt", "--kind", "rule", "--body", "Format before commit.", "--description", "d")
	out := filepath.Join(t.TempDir(), "rules.md")
	captureRun(t, "export", "--tag", "rule", "--out", out)

	// Fresh export: no drift.
	if doc := captureRun(t, "doctor"); strings.Contains(doc, "export drift") {
		t.Fatalf("fresh export must not report drift:\n%s", doc)
	}

	// The store changes → the compiled file is stale, and the warning carries
	// the copy-pasteable refresh command.
	captureRun(t, "note", "Table tests", "--kind", "rule", "--body", "One case per row.", "--description", "d")
	doc := captureRun(t, "doctor")
	if !strings.Contains(doc, "export drift") {
		t.Fatalf("doctor should flag a stale export after the store changed:\n%s", doc)
	}
	if !strings.Contains(doc, "nt export --tag rule") || !strings.Contains(doc, out) {
		t.Fatalf("drift warning should include the refresh command and target:\n%s", doc)
	}

	// Re-export → healthy again.
	captureRun(t, "export", "--tag", "rule", "--out", out)
	if doc := captureRun(t, "doctor"); strings.Contains(doc, "export drift") {
		t.Fatalf("re-export should clear the drift warning:\n%s", doc)
	}

	// A hand-edited target drifts too — and the warning says a re-export would
	// overwrite those edits instead of silently clobbering them later.
	if err := os.WriteFile(out, []byte("# hand-tuned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc = captureRun(t, "doctor")
	if !strings.Contains(doc, "export drift") || !strings.Contains(doc, "edited outside nt export") {
		t.Fatalf("hand-edited target should warn about the overwrite:\n%s", doc)
	}

	// A deleted target is reported as gone, not silently untracked.
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	if doc := captureRun(t, "doctor"); !strings.Contains(doc, "is gone") {
		t.Fatalf("doctor should report a missing export target:\n%s", doc)
	}
}

// Stdout exports are ephemeral by nature — only --out targets are tracked.
func TestExportStdoutIsNotTracked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Run gofmt", "--kind", "rule", "--body", "Format first.", "--description", "d")
	captureRun(t, "export", "--tag", "rule")
	if _, err := os.Stat(filepath.Join(dir, exportStateFile)); !os.IsNotExist(err) {
		t.Fatalf("stdout export must not create %s (err=%v)", exportStateFile, err)
	}
}
