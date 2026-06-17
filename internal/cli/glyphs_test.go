package cli

import (
	"strings"
	"testing"
)

// TestAsciiModeTaskStatusIcon: with NT_ASCII=1 the task list renders the ASCII
// checkbox status icon and not the unicode glyph.
func TestAsciiModeTaskStatusIcon(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_ASCII", "1")
	captureRun(t, "add", "write the report")

	out := captureRun(t, "list")
	if !strings.Contains(out, "[ ]") {
		t.Fatalf("NT_ASCII list should use the ASCII open marker [ ]:\n%s", out)
	}
	if strings.ContainsAny(out, "○✓◐⊘") {
		t.Fatalf("NT_ASCII list must not contain unicode status glyphs:\n%s", out)
	}
}

// TestAsciiModeReportMarker: with NT_ASCII=1 the recall note marker uses the
// ASCII form, not the unicode glyph.
func TestAsciiModeReportMarker(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NT_ASCII", "1")
	captureRun(t, "note", "Auth design", "--folder", "ref", "--body", "x")

	out := captureRun(t, "recall")
	if strings.ContainsRune(out, '▤') {
		t.Fatalf("NT_ASCII recall must not contain the unicode note glyph:\n%s", out)
	}
	if !strings.Contains(out, "- ") || !strings.Contains(out, "Auth design") {
		t.Fatalf("NT_ASCII recall should mark the note with an ASCII '-':\n%s", out)
	}
}

// TestNoColorTriggersAscii: NO_COLOR (any value) also switches to ASCII output.
func TestNoColorTriggersAscii(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	t.Setenv("NO_COLOR", "")
	captureRun(t, "add", "write the report")

	out := captureRun(t, "list")
	if !strings.Contains(out, "[ ]") {
		t.Fatalf("NO_COLOR list should use the ASCII open marker [ ]:\n%s", out)
	}
	if strings.ContainsAny(out, "○✓◐⊘") {
		t.Fatalf("NO_COLOR list must not contain unicode status glyphs:\n%s", out)
	}
}

// TestDefaultStaysUnicode: with neither env set, output keeps the unicode
// glyphs (so existing behavior and tests are unchanged).
func TestDefaultStaysUnicode(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "add", "write the report")

	out := captureRun(t, "list")
	if !strings.ContainsRune(out, '○') {
		t.Fatalf("default list should keep the unicode open glyph:\n%s", out)
	}
	if strings.Contains(out, "[ ]") {
		t.Fatalf("default list must not use the ASCII marker:\n%s", out)
	}
}
