package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nt import's JSON path is the inverse of `nt export --format json`: export
// from one store, import into another, and the content round-trips (a new
// id is expected — import always mints fresh ones).
func TestImportJSONRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	t.Setenv("NT_DIR", srcDir)
	captureRun(t, "note", "Run gofmt", "--body", "Always format before commit.", "--tag", "rule", "--description", "formatting rule")
	dest := filepath.Join(t.TempDir(), "backup.json")
	captureRun(t, "export", "--tag", "rule", "--format", "json", "--out", dest)

	dstDir := t.TempDir()
	t.Setenv("NT_DIR", dstDir)
	out := captureRun(t, "import", dest)
	if !strings.Contains(out, "imported 1 note") {
		t.Fatalf("expected 1 note imported:\n%s", out)
	}
	list := captureRun(t, "search", "--tag", "rule", "--type", "note")
	if !strings.Contains(list, "Run gofmt") {
		t.Fatalf("imported note should be findable in the new store:\n%s", list)
	}
}

// nt's note format already reads Obsidian's frontmatter conventions, so a
// folder of plain markdown files (an exported/synced Obsidian vault) should
// import cleanly, tags and all.
func TestImportMarkdownVault(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "auth-notes.md"), []byte("---\ntags: [auth, backend]\n---\n# JWT lifetime\n\nTokens expire after 24h.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vault, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".obsidian", "workspace.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NT_DIR", t.TempDir())
	out := captureRun(t, "import", vault, "--tag", "from-vault")
	if !strings.Contains(out, "imported 1 note") {
		t.Fatalf("expected 1 note imported from the vault (hidden .obsidian/ skipped):\n%s", out)
	}
	got := captureRun(t, "search", "--tag", "auth", "--type", "note")
	if !strings.Contains(got, "JWT lifetime") {
		t.Fatalf("vault note should be imported with its frontmatter tags:\n%s", got)
	}
	got = captureRun(t, "search", "--tag", "from-vault", "--type", "note")
	if !strings.Contains(got, "JWT lifetime") {
		t.Fatalf("--tag should stamp every imported note:\n%s", got)
	}
}

func TestImportSkipsNearDuplicatesUnlessForced(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	captureRun(t, "note", "Run gofmt", "--body", "existing note")

	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "gofmt.md"), []byte("# Run gofmt\n\nduplicate import\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureRun(t, "import", vault)
	if !strings.Contains(out, "skipped 1 near-duplicate") {
		t.Fatalf("expected the near-duplicate title to be skipped:\n%s", out)
	}
	out = captureRun(t, "import", vault, "--force")
	if !strings.Contains(out, "imported 1 note") {
		t.Fatalf("--force should import the near-duplicate anyway:\n%s", out)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "n.md"), []byte("# Dry run note\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureRun(t, "import", vault, "--dry-run")
	if !strings.Contains(out, "would import 1 note") {
		t.Fatalf("expected a dry-run report:\n%s", out)
	}
	got := captureRun(t, "search", "Dry run note", "--type", "note")
	if strings.Contains(got, "Dry run note") {
		t.Fatalf("--dry-run must not write: %s", got)
	}
}

func TestImportNoArgsIsUsageError(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	if _, code := runWithStdout("import"); code != 2 {
		t.Fatalf("import with no path should be a usage error, got code %d", code)
	}
}
