package recall

import (
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// Tier boundaries — the numbers a field report can point at when a hit is
// mislabeled. Calibrated against the paraphrase corpus (below) and the
// real-store tailwind/dark-mode acceptance case (internal/cli, manually
// verified); recalibrate here, not by scattering new magic numbers.
func TestTierBoundaries(t *testing.T) {
	cases := []struct {
		conf float64
		want string
	}{
		{0.90, "strong"}, {0.55, "strong"}, {0.549, "medium"},
		{0.20, "medium"}, {0.15, "medium"}, {0.149, "weak"}, {0.0, "weak"},
	}
	for _, c := range cases {
		r := Result{Confidence: c.conf}
		if got := r.Tier(); got != c.want {
			t.Errorf("Tier(%.3f) = %q, want %q", c.conf, got, c.want)
		}
	}
}

// An exact-title match on every query word is the ceiling: confidence must
// land at (near) 1.0, and — this is the point of the whole feature — a
// one-concept match against a long, specific query must read as weak, not
// silently score a big-looking number.
func TestConfidenceCeilingAndFloor(t *testing.T) {
	notes := []*note.Note{
		mk("Goroutine deadlock on shared client mutex", "held across a channel send", "lesson"),
	}
	got := Rank(notes, "goroutine deadlock shared client mutex", 5)
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %d", len(got))
	}
	if got[0].Confidence < 0.9 {
		t.Errorf("exact title match on every term: confidence = %.3f, want ~1.0", got[0].Confidence)
	}
	if got[0].Tier() != "strong" {
		t.Errorf("exact title match: tier = %q, want strong", got[0].Tier())
	}
}

// The lesson/project boosts must NOT leak into Confidence — a thinly-matched
// lesson (one shared concept on a long query) printing "strong" is the exact
// promotion pathology the precision floor already fights; folding boosts
// into confidence would reopen it under a new name.
func TestConfidenceExcludesBoosts(t *testing.T) {
	notes := []*note.Note{
		mk("Cache invalidation notes", "invalidation details here", "lesson", "cache"),
	}
	// A 5-word query sharing just one concept ("cache") with a lesson note:
	// the lesson boost (1.6x) inflates Score, but Confidence must stay low —
	// it's computed pre-boost.
	got := Rank(notes, "cache warm restart deploy pipeline", 5)
	if len(got) != 1 {
		t.Fatalf("want 1 result (single-shared-concept fallback), got %d", len(got))
	}
	if got[0].Tier() == "strong" {
		t.Errorf("one-concept match on a 5-term query boosted by lesson tag reads as strong (conf %.3f) — boosts leaked into confidence", got[0].Confidence)
	}
}

// An all-noise store must never present as strong: tiers are absolute
// (query-normalized), so no ratio-to-top-hit ever promotes the best piece of
// noise — the failure mode this whole feature exists to kill.
func TestAllNoiseStoreIsNeverStrong(t *testing.T) {
	notes := []*note.Note{
		mk("Grocery list", "milk eggs bread butter", "personal"),
		mk("Editor keybindings", "vim motions and macros", "note"),
	}
	got := Rank(notes, "how to configure tailwind dark mode responsively", 5)
	for _, r := range got {
		if r.Tier() == "strong" {
			t.Errorf("noise result %q presents as strong (conf %.3f)", r.Note.Title, r.Confidence)
		}
	}
}
