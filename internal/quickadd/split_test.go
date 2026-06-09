package quickadd

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitLong(t *testing.T) {
	// Normal tasks are never split.
	if _, _, ok := SplitLong("buy milk and eggs"); ok {
		t.Error("a short task should not split")
	}

	long := "Investigate and resolve the intermittent 500 errors occurring during peak " +
		"traffic by correlating traces across the gateway and token service, validating " +
		"the connection-pool-exhaustion hypothesis, and coordinating with the infra team."
	title, body, ok := SplitLong(long)
	if !ok {
		t.Fatal("a paragraph-length task should split")
	}
	if n := utf8.RuneCountInString(title); n > 73 {
		t.Errorf("title should be short, got %d runes: %q", n, title)
	}
	if strings.Contains(title, "…") {
		t.Errorf("title is a real title, not a display truncation: %q", title)
	}
	if body != long {
		t.Error("body should preserve the full original text")
	}
	if !strings.HasPrefix(long, title) {
		t.Errorf("title should be a clean prefix of the text: %q", title)
	}
}

func TestClauseTitle(t *testing.T) {
	// Cuts at the first sentence boundary (input longer than max so it truncates).
	long := "Fix the auth bug now. Then write the regression tests and deploy to staging and then production over the weekend window."
	if got := clauseTitle(long, 72); got != "Fix the auth bug now" {
		t.Errorf("sentence cut: got %q", got)
	}
	// Falls back to a word boundary when there's no early clause mark.
	got := clauseTitle(strings.Repeat("alpha ", 40), 30)
	if utf8.RuneCountInString(got) > 30 || strings.HasSuffix(got, " ") || strings.Contains(got, "…") {
		t.Errorf("word cut: %q", got)
	}
	// Unicode-safe (no panic / valid cut).
	if got := clauseTitle("Café résumé planning "+strings.Repeat("naïve ", 30), 24); utf8.RuneCountInString(got) > 24 {
		t.Errorf("unicode cut too long: %q", got)
	}
}
