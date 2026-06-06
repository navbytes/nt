package task

import "testing"

func mustParse(t *testing.T, line string) *Task {
	t.Helper()
	tk, ok := ParseLine(line)
	if !ok {
		t.Fatalf("ParseLine(%q) failed", line)
	}
	return tk
}

func TestCompletedSince(t *testing.T) {
	tasks := []*Task{
		mustParse(t, "x 2026-06-05 write docs"),
		mustParse(t, "(A) open task"),             // not done → excluded
		mustParse(t, "x 2026-06-06 ship release"), // newest
		mustParse(t, "x 2026-06-01 old thing"),    // oldest
		mustParse(t, "x deploy without a date"),   // done, no completion date
	}

	all := CompletedSince(tasks, "")
	if len(all) != 4 {
		t.Fatalf("expected 4 completed, got %d", len(all))
	}
	// Newest completion first; the undated one sorts last.
	if all[0].Text != "ship release" || all[1].Text != "write docs" || all[2].Text != "old thing" {
		t.Fatalf("wrong order: %q, %q, %q", all[0].Text, all[1].Text, all[2].Text)
	}
	if all[3].Text != "deploy without a date" {
		t.Fatalf("undated completion should sort last, got %q", all[3].Text)
	}

	// since bound includes only on/after the date and drops undated completions.
	since := CompletedSince(tasks, "2026-06-05")
	if len(since) != 2 {
		t.Fatalf("since 2026-06-05 should yield 2, got %d", len(since))
	}
	if since[0].Text != "ship release" || since[1].Text != "write docs" {
		t.Fatalf("since order wrong: %q, %q", since[0].Text, since[1].Text)
	}
}
