package tui

import (
	"strings"
	"testing"
)

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		hay, filter string
		want        bool
	}{
		{"fix my bug", "fmb", true},              // abbreviation as a subsequence
		{"fix the bug", "fix bug", true},         // space-separated terms
		{"fix the bug", "bug fix", true},         // terms are order-independent
		{"refactor auth flow", "authref", false}, // out-of-order within one term fails
		{"refactor auth flow", "ref auth", true}, // …but as separate terms it matches
		{"buy milk", "xyz", false},
		{"anything", "", true},           // empty filter matches everything
		{"anything", "   ", true},        // whitespace-only filter matches
		{"Fix The Bug", "fix bug", true}, // case-insensitive
		{"café au lait", "caf", true},    // unicode haystack
		{"write the spec", "wts", true},  // first-letters subsequence
		{"deadline today", "dead", true}, // plain prefix still works
		{"abc", "abcd", false},           // needle longer than any match
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.hay, c.filter); got != c.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", c.hay, c.filter, got, c.want)
		}
	}
}

// TestFuzzyFilterFindsTasks drives the matcher through the real task list: "fab"
// is a subsequence of "fix auth bug" (one of the seeded tasks) but not a
// substring, so it proves the live filter now matches fuzzily end-to-end.
func TestFuzzyFilterFindsTasks(t *testing.T) {
	m := testModel(t) // seeds: "fix auth bug …", "write tests …", "deploy"

	m.filter = "fab"
	m.rebuild()
	if len(m.flat) != 1 || !strings.Contains(m.flat[0].Line(), "fix auth bug") {
		t.Fatalf("fuzzy filter should isolate 'fix auth bug', got %d: %+v", len(m.flat), m.flat)
	}

	m.filter = "zzz" // matches nothing
	m.rebuild()
	if len(m.flat) != 0 {
		t.Errorf("no task should match 'zzz', got %d", len(m.flat))
	}
}
