package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Config [decay] supplies a per-kind half-life default, stamped into the new
// note's frontmatter (visible, editable) — decay policy set once instead of
// re-judged per note. An explicit --half-life wins; a typo'd config value
// stamps nothing and is doctor's to report, never capture's to fail on.
func TestNoteKindHalfLifeConfigDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	cfg := "[decay]\nlesson = \"90d\"\nref = \"oops\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	mustFile := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, "notes", rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	// Kind default applies when no --half-life was given.
	captureRun(t, "note", "vitest mocks leak", "--kind", "lesson", "--tag", "vitest", "--description", "d")
	if got := mustFile("lessons/vitest-mocks-leak.md"); !strings.Contains(got, "half_life: 90d") {
		t.Fatalf("kind default should stamp half_life into frontmatter:\n%s", got)
	}

	// An explicit --half-life beats the config default.
	captureRun(t, "note", "flag parsing gotcha", "--kind", "lesson", "--tag", "cli", "--half-life", "30d", "--description", "d")
	if got := mustFile("lessons/flag-parsing-gotcha.md"); !strings.Contains(got, "half_life: 30d") {
		t.Fatalf("explicit --half-life should win over the config default:\n%s", got)
	}

	// An unparseable config value stamps nothing…
	captureRun(t, "note", "auth flow overview", "--kind", "ref", "--tag", "auth", "--description", "d")
	if got := mustFile("ref/auth-flow-overview.md"); strings.Contains(got, "half_life") {
		t.Fatalf("invalid config default must stamp nothing:\n%s", got)
	}
	// …and doctor reports the typo.
	if doc := captureRun(t, "doctor"); !strings.Contains(doc, "[decay] ref") {
		t.Fatalf("doctor should flag the invalid [decay] value:\n%s", doc)
	}

	// A kind with no configured default stays undecayed.
	captureRun(t, "note", "Chose flock over sqlite", "--kind", "decision", "--tag", "storage", "--description", "d")
	if got := mustFile("decisions/chose-flock-over-sqlite.md"); strings.Contains(got, "half_life") {
		t.Fatalf("kind without a configured default must not decay:\n%s", got)
	}
}
