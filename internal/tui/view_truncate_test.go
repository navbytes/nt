package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/navbytes/nt/internal/task"
)

// TestTruncateDisplayWidth guards the emoji/CJK case: truncate must cut by
// display width, so a double-width rune can't push the result past max and wrap
// the line (which broke the notes-list split-pane divider).
func TestTruncateDisplayWidth(t *testing.T) {
	got := truncate("Café ☕ Meeting with the whole leadership team", 12)
	if w := lipgloss.Width(got); w > 12 {
		t.Errorf("truncate width = %d, want ≤ 12: %q", w, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected an ellipsis, got %q", got)
	}
	if truncate("short", 20) != "short" {
		t.Errorf("a string within max should pass through unchanged")
	}
	if truncate("xxxxxxxxxx", 5) != "xxxx…" {
		t.Errorf("ascii truncation: got %q, want %q", truncate("xxxxxxxxxx", 5), "xxxx…")
	}
}

// TestMetaStrMarksRecurring: recurring tasks carry a ↻ glyph so they read
// distinctly from one-offs. ParseLine (not New) populates the rec: kv.
func TestMetaStrMarksRecurring(t *testing.T) {
	rec, ok := task.ParseLine("water plants rec:3d id:01J9ZZZZZZZZZZZZZZZZZZZZZZ")
	if !ok {
		t.Fatal("failed to parse recurring task line")
	}
	if got := metaStr(rec); !strings.Contains(got, "↻") {
		t.Errorf("recurring task should show ↻, got %q", got)
	}

	one, ok := task.ParseLine("a one-off task id:01J9YYYYYYYYYYYYYYYYYYYYYY")
	if !ok {
		t.Fatal("failed to parse one-off task line")
	}
	if got := metaStr(one); strings.Contains(got, "↻") {
		t.Errorf("non-recurring task should not show ↻, got %q", got)
	}
}
