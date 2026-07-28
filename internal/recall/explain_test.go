package recall

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// mkID is mk plus an explicit ID, needed wherever a test targets one note by
// ID (ExplainNote) — mk alone leaves ID "" since ranking never reads it.
func mkID(id, title, body string, tags ...string) *note.Note {
	n := mk(title, body, tags...)
	n.ID = id
	return n
}

// ExplainProject must never change what gets returned or in what order —
// it's the same scorer with an optional side channel, not a second one.
func TestExplainMatchesRank(t *testing.T) {
	notes := corpusNotes()
	for _, c := range paraphraseCases {
		want := Rank(notes, c.query, 5)
		got, trace := ExplainProject(notes, c.query, 5, "")
		if trace == nil {
			t.Fatalf("%q: ExplainProject returned a nil trace", c.query)
		}
		if len(got) != len(want) {
			t.Fatalf("%q: result count diverged: explain %d vs rank %d", c.query, len(got), len(want))
		}
		for i := range got {
			if got[i].Note.Title != want[i].Note.Title || got[i].Score != want[i].Score {
				t.Errorf("%q: result %d diverged: explain %+v vs rank %+v", c.query, i, got[i], want[i])
			}
		}
	}
}

// The per-term hit kinds must match what actually scored: exact word in the
// strong bag (title/tags/description) vs. a same-concept synonym, strong vs.
// weak (body) field.
func TestExplainHitKinds(t *testing.T) {
	notes := []*note.Note{
		mkID("n1", "Goroutine deadlock on shared client", "held a mutex across a channel send", "lesson"),
	}
	_, trace := ExplainProject(notes, "goroutine parallel deadlock", 5, "")
	if len(trace.Notes) != 1 {
		t.Fatalf("want 1 traced note, got %d", len(trace.Notes))
	}
	hits := trace.Notes[0].Hits
	byTerm := map[string]TermHit{}
	for _, h := range hits {
		byTerm[h.Term] = h
	}
	// Trace terms are stemmed (same as the scorer), so look them up stemmed too.
	// "goroutine" is a literal word in the title (strong bag) -> strong-exact.
	if h := byTerm[stem("goroutine")]; h.Where != "strong-exact" {
		t.Errorf("goroutine: Where = %q, want strong-exact", h.Where)
	}
	// "deadlock" is also a literal title word -> strong-exact.
	if h := byTerm[stem("deadlock")]; h.Where != "strong-exact" {
		t.Errorf("deadlock: Where = %q, want strong-exact", h.Where)
	}
	// "parallel" shares goroutine's synonym group but isn't itself in the
	// title -> strong-syn (the group matched a strong-bag word).
	if h := byTerm[stem("parallel")]; h.Where != "strong-syn" {
		t.Errorf("parallel: Where = %q, want strong-syn", h.Where)
	}
}

// The precision floor, tail trim, and --limit must each tag what they drop
// with a reason, not just silently remove it — the whole point of --explain
// is that a missing note is as much the story as a present one.
func TestExplainTagsExclusionReasons(t *testing.T) {
	notes := []*note.Note{
		mkID("hit1", "Concurrent writers time out behind the store lock", "goroutines serialize behind a mutex", "lesson"),
		mkID("hit2", "Deploy rollout needs a flag", "production canary release notes", "lesson"),
		// Shares exactly ONE query concept ("lock", body-only) — the precision
		// floor should drop this once hit1 clears 2 matched terms.
		mkID("floored", "Weekly planning", "review the board, lock the sprint scope, reschedule anything stale", "note"),
	}
	// 4-word query: "concurrent"/"writers"/"timeout"/"lock" — hit1 matches
	// several strong-bag terms; "floored" shares only "store"-adjacent noise.
	_, trace := ExplainProject(notes, "concurrent writers timeout lock", 5, "")
	if !trace.FloorActive {
		t.Fatalf("expected the precision floor to activate, trace: %+v", trace)
	}
	var sawFloored bool
	for _, nt := range trace.Notes {
		if nt.ID == "floored" {
			sawFloored = true
			if nt.Excluded != "precision-floor" {
				t.Errorf("floored note excluded reason = %q, want precision-floor", nt.Excluded)
			}
		}
	}
	if !sawFloored {
		t.Fatalf("expected the floored candidate to appear in the excluded section, got: %+v", trace.Notes)
	}
}

// A zero-overlap note is never a RankProject candidate at all, so ExplainNote
// is the only way to see why — it must trace the note anyway, tagged
// "no-match", with the note's strong-bag vocabulary (the actionable half of
// "why is it missing").
func TestExplainNoteZeroOverlap(t *testing.T) {
	notes := []*note.Note{
		mkID("css1", "Flexbox columns overflow their container", "set min-width:0 on flex items", "lesson", "css"),
	}
	trace, err := ExplainNote(notes, "postgres migration lock_timeout", "", "css1")
	if err != nil {
		t.Fatalf("ExplainNote: %v", err)
	}
	if len(trace.Notes) != 1 {
		t.Fatalf("want 1 traced note, got %d", len(trace.Notes))
	}
	nt := trace.Notes[0]
	if nt.Excluded != "no-match" {
		t.Errorf("Excluded = %q, want no-match", nt.Excluded)
	}
	if len(nt.StrongTerms) == 0 {
		t.Error("StrongTerms empty for the ExplainNote target")
	}
	if !strings.Contains(strings.Join(nt.StrongTerms, " "), "flexbox") {
		t.Errorf("StrongTerms %v missing the note's own title word", nt.StrongTerms)
	}
}

// An unknown id must error, not silently return an empty trace.
func TestExplainNoteUnknownID(t *testing.T) {
	notes := []*note.Note{mkID("a", "Some note", "body text", "note")}
	if _, err := ExplainNote(notes, "anything at all", "", "does-not-exist"); err == nil {
		t.Error("want an error for an unknown note id, got nil")
	}
}
