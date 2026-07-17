package note

import (
	"strings"
	"testing"
)

// The path-style shorthand's boundary: a slash is filing syntax only when the
// prefix before the last slash is one whitespace-free token and a non-empty
// title remains after it. Regression for the field failure where a prose title
// ending in a path ("… valid at .claude/company/release-2.0-search/")
// was split into folder + empty title and rejected with "a title is required".
func TestSplitPathTitle(t *testing.T) {
	prose := "Search (2.0) SHELVED pre-build — design valid at .claude/company/release-2.0-search/"
	interior := "Deploy lag: smoke tests must poll apps/web after deploy"
	cases := []struct{ raw, folder, title string }{
		// filing syntax: single-token prefix + remaining title
		{"work/Auth design", "work", "Auth design"},
		{"work/auth/Design v2", "work/auth", "Design v2"},
		{"custom/x", "custom", "x"},
		{"  work/Auth  ", "work", "Auth"},
		// no slash
		{"plain title", "", "plain title"},
		{"", "", ""},
		// prose prefix (whitespace before the last slash) — never a filing choice
		{"a b/c", "", "a b/c"},
		{"work /x", "", "work /x"},
		{interior, "", interior},
		// trailing slash — no title left after it, whole string stays the title
		{"docs/", "", "docs/"},
		{prose, "", prose},
		// leading or lone slash — empty prefix
		{"/x", "", "/x"},
		{"/", "", "/"},
	}
	for _, c := range cases {
		f, ti := SplitPathTitle(c.raw)
		if f != c.folder || ti != c.title {
			t.Errorf("SplitPathTitle(%q) = (%q, %q), want (%q, %q)", c.raw, f, ti, c.folder, c.title)
		}
	}
}

// Slug caps its output well under the filesystem's 255-byte filename limit so
// long prose titles (which the folder shorthand no longer truncates) still
// yield a creatable file, cut at a word boundary.
func TestSlugCapsLongTitles(t *testing.T) {
	slug := Slug(strings.Repeat("wordy title segment ", 20))
	if len(slug) == 0 || len(slug) > 120 {
		t.Fatalf("capped slug length = %d, want 1..120: %q", len(slug), slug)
	}
	if strings.HasSuffix(slug, "-") {
		t.Fatalf("capped slug ends in dash: %q", slug)
	}
}

// End-to-end: a very long slashless title must create a file — used to fail
// with "file name too long" before the slug cap.
func TestCreateLongTitle(t *testing.T) {
	s := testStore(t)
	long := "long title " + strings.Repeat("with many words ", 25) + "end"
	if _, err := Create(s, long, "", nil, "cli", ""); err != nil {
		t.Fatalf("Create(long title): %v", err)
	}
}
