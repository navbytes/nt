package cli

import (
	"strings"
	"testing"
)

// TestShortIdWikilinkProducesBacklink guards a documented-but-broken promise:
// SKILL.md says "[[note-slug]] or [[<id>]] … backlinks are found automatically",
// and Resolve accepts the 6-char short id nt prints everywhere — but backlink
// discovery matched only the full ULID, so an id-form link resolved forward and
// left the target showing "linked from: (none)".
func TestShortIdWikilinkProducesBacklink(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Target Note", "--body", "the thing being linked to")

	// Grab the short id nt printed for the target.
	out := captureRun(t, "notes")
	var short string
	for _, f := range strings.Fields(out) {
		if len(f) == 6 {
			short = f
			break
		}
	}
	if short == "" {
		t.Fatalf("could not find a short id in: %s", out)
	}

	captureRun(t, "note", "Source Note", "--body", "see [["+short+"]] for detail")

	if got := captureRun(t, "links", "Target Note"); !strings.Contains(strings.ToLower(got), "source-note") {
		t.Errorf("an id-form [[%s]] link should register as a backlink, got:\n%s", short, got)
	}
}

// TestSlugLikeWikilinkIsNotMistakenForAnId: the short-id match is restricted to
// Crockford base32 (no I/L/O/U) so an ordinary slug can't be read as an id
// suffix by backlink discovery, which has no note list to disambiguate with.
func TestSlugLikeWikilinkIsNotMistakenForAnId(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Alpha", "--body", "target")
	captureRun(t, "note", "Beta", "--body", "mentions [[quiz]] which is not an id")

	if got := captureRun(t, "links", "Alpha"); strings.Contains(strings.ToLower(got), "beta") {
		t.Errorf("a non-id slug link must not produce a spurious backlink:\n%s", got)
	}
}
