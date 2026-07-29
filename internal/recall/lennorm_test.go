package recall

import (
	"math"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// lenNormFixtureNotes is a length-variance corpus — some notes short, some
// bodies padded well past their strong bag — so a leak in the length
// normalization path (norm != 1 when b should be off) would move a Score or
// Confidence value, not just a ranking.
func lenNormFixtureNotes() []*note.Note {
	return []*note.Note{
		mk("Goroutine deadlock on shared client", "A mutex was held across a channel send while another goroutine waited on the same lock, and the deadlock only reproduced under load because the fast path never contended for it in dev.", "lesson", "concurrency"),
		mk("Deploy needs the --confirm flag", strings.Repeat("Production rollout is a no-op without it. ", 40), "lesson", "deploy"),
		mk("Grocery list", "milk and eggs", "personal"),
		mk("Cache invalidation on writes", strings.Repeat("Stale reads persisted because the cache key did not include the tenant id, so invalidation missed cross-tenant writes entirely. ", 20), "lesson", "cache"),
		mk("Short auth note", "JWT expiry.", "auth"),
	}
}

// lenNormFixtureQueries pairs each query with the exact (Score, Confidence,
// Matched, QueryTerms) recall.Rank produced on main before this change,
// captured by running main's recall.Rank against this same corpus in
// isolation (see the branch's PR description for how). Any drift here at
// NT_LENNORM_B unset means the b=0 path stopped being a no-op.
var lenNormFixtureQueries = []struct {
	query                     string
	wantTitle                 string
	wantScore                 int
	wantConfidence            float64
	wantMatched, wantQueryLen int
}{
	{"adding parallel request handling with async workers", "Goroutine deadlock on shared client", 1147, 0.2, 2, 5},
	{"how do I release to prod safely", "Deploy needs the --confirm flag", 1147, 0.3333333333333333, 2, 3},
	{"cache keys and invalidation across tenants", "Cache invalidation on writes", 4587, 0.8, 4, 5},
	{"jwt token expiry", "Short auth note", 1433, 0.6666666666666666, 2, 3},
	{"grocery shopping", "Grocery list", 717, 0.5, 1, 2},
}

// TestLenNormZeroIsIdentical proves NT_LENNORM_B's default (unset, "0", and
// anything else that clamps to 0) reproduces the shipped pre-normalization
// scorer bit-for-bit — the whole point of shipping this default-off. Values
// were captured from main's recall.Rank (see lenNormFixtureQueries) so this
// pins actual production output, not just internal self-consistency.
func TestLenNormZeroIsIdentical(t *testing.T) {
	for _, envVal := range []string{"", "0", "-5", "not-a-number"} {
		t.Run("env="+envVal, func(t *testing.T) {
			if envVal == "" {
				t.Setenv("NT_LENNORM_B", "")
			} else {
				t.Setenv("NT_LENNORM_B", envVal)
			}
			notes := lenNormFixtureNotes()
			for _, c := range lenNormFixtureQueries {
				got := Rank(notes, c.query, 10)
				if len(got) == 0 || got[0].Note.Title != c.wantTitle {
					t.Fatalf("%q: want top %q, got %v", c.query, c.wantTitle, titles(got))
				}
				r := got[0]
				if r.Score != c.wantScore {
					t.Errorf("%q: Score = %d, want %d", c.query, r.Score, c.wantScore)
				}
				if r.Confidence != c.wantConfidence {
					t.Errorf("%q: Confidence = %.10f, want %.10f", c.query, r.Confidence, c.wantConfidence)
				}
				if r.Matched != c.wantMatched || r.QueryTerms != c.wantQueryLen {
					t.Errorf("%q: Matched/QueryTerms = %d/%d, want %d/%d", c.query, r.Matched, r.QueryTerms, c.wantMatched, c.wantQueryLen)
				}
			}
		})
	}
}

// TestParaphraseCorpusAcrossB re-runs the paraphrase corpus's HIT@1 floor at
// each shipped-relevant b, per docs/memory-integration-roadmap.md item 13:
// per-bag normalization must not regress the corpus at any of these values,
// unlike the combined-bag mode that dropped it to 7/8.
func TestParaphraseCorpusAcrossB(t *testing.T) {
	for _, b := range []string{"0", "0.5", "1.0"} {
		t.Run("b="+b, func(t *testing.T) {
			t.Setenv("NT_LENNORM_B", b)
			notes := corpusNotes()
			hits := 0
			for _, c := range paraphraseCases {
				got := Rank(notes, c.query, 5)
				if len(got) > 0 && got[0].Note.Title == c.want {
					hits++
				}
			}
			if hits < minHitAtOne {
				t.Errorf("b=%s: HIT@1 = %d/%d, want >= %d", b, hits, len(paraphraseCases), minHitAtOne)
			}
		})
	}
}

// TestLenNormOutOfRangeClamps pins the documented clamp behaviour: b outside
// [0,1] clamps to the nearest bound rather than erroring or passing through.
// b=1 lands notes purely on relative concept-count ratios (no additive term),
// so a value like "5" must score identically to "1".
func TestLenNormOutOfRangeClamps(t *testing.T) {
	notes := lenNormFixtureNotes()
	q := "cache keys and invalidation across tenants"
	t.Setenv("NT_LENNORM_B", "1")
	want := Rank(notes, q, 10)
	for _, over := range []string{"5", "100"} {
		t.Setenv("NT_LENNORM_B", over)
		got := Rank(notes, q, 10)
		if len(got) != len(want) || len(got) == 0 || got[0].Score != want[0].Score {
			t.Errorf("NT_LENNORM_B=%s not clamped to 1: got %v, want %v", over, got, want)
		}
	}
}

// TestLenNormPerBagDivisorMath pins the per-bag divisor arithmetic
// (1-b + b*dl/avgdl per bag, floored at 1e-9) against a hand-computed
// expectation on a tiny synthetic corpus, so a future refactor can't
// silently change which average a bag is normalized against.
//
// Both notes share an identical first body line ("filler filler filler.") so
// note.Description(240) — which recall folds into the STRONG bag — always
// contributes the same concept count on both sides; the only thing that
// differs is each note's WEAK (body) bag size, isolating exactly the divisor
// this test means to pin. "zeta" only appears in titles (strong-exact);
// "omega" only appears in bodies (weak-exact) — never both — so each query
// term hits exactly one bag on both notes.
func TestLenNormPerBagDivisorMath(t *testing.T) {
	notes := []*note.Note{
		mk("Zeta short", "filler filler filler.\nomega"),
		mk("Zeta long", "filler filler filler.\nomega omega2 omega3 omega4"),
	}
	t.Setenv("NT_LENNORM_B", "1")
	got := Rank(notes, "zeta omega", 10)
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	byTitle := map[string]Result{}
	for _, r := range got {
		byTitle[r.Note.Title] = r
	}
	short, long := byTitle["Zeta short"], byTitle["Zeta long"]
	// Strong bags: {zeta, short|long, filler} — 3 concepts each, so
	// avgStrong=3 and strongNorm=3/3=1 for both (b=1 makes the divisor
	// exactly dl/avgdl). The "zeta" hit is worth the same 4*idf(zeta) on
	// both notes; this test is entirely about the weak-bag term.
	//
	// Weak bags: short={filler,omega} (2 concepts), long={filler,omega,
	// omega2,omega3,omega4} (5 concepts). avgWeak=(2+5)/2=7/2.
	// weakNorm short = 2/(7/2) = 4/7; weakNorm long = 5/(7/2) = 10/7.
	// "omega" base=2, so its normalized contribution is 2/(4/7)=7/2 on
	// short and 2/(10/7)=7/5 on long.
	//
	// "zeta" and "omega" both have document frequency 2 (both notes contain
	// both), so idf(zeta)==idf(omega) and cancels out of the ratio:
	// short_raw/long_raw = (4 + 7/2) / (4 + 7/5) = (15/2) / (27/5) = 25/18.
	const wantRatio = 25.0 / 18.0
	ratio := float64(short.Score) / float64(long.Score)
	if math.Abs(ratio-wantRatio) > 0.005 {
		t.Errorf("short/long score ratio = %.4f, want %.4f (25/18) per the per-bag divisor math", ratio, wantRatio)
	}
}
