package recall

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
)

// evalQuery is one row of the external JSON corpus read by
// TestRealStoreHitAtOne. The corpus format is fixed:
//
// targetID is the FULL 26-char ULID from the note's frontmatter, not the
// 6-char short handle nt prints — matching is exact, so a short form never
// resolves. The id below is synthetic; a real one would name a real note.
//
//	[ { "query": "plain-words question a future session would ask",
//	    "targetID": "01HZZZZZZZZZZZZZZZZZZZZZZZ",
//	    "targetTitle": "exact title of the note that should rank #1" } ]
type evalQuery struct {
	Query       string `json:"query"`
	TargetID    string `json:"targetID"`
	TargetTitle string `json:"targetTitle"`
}

// publishedEvalSizes are the corpus sizes behind the curve published in
// docs/memory-integration-roadmap.md item 13. 0 means "the whole store",
// whatever size that currently is. Override with NT_EVAL_SIZES (a
// comma-separated int list) to reproduce a different curve.
var publishedEvalSizes = []int{22, 40, 100, 160, 218, 0}

// evalSizes returns publishedEvalSizes, or the sizes from NT_EVAL_SIZES when
// set.
func evalSizes(t *testing.T) []int {
	t.Helper()
	raw := os.Getenv("NT_EVAL_SIZES")
	if raw == "" {
		return publishedEvalSizes
	}
	var sizes []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			t.Fatalf("NT_EVAL_SIZES %q: %q is not an integer", raw, part)
		}
		sizes = append(sizes, n)
	}
	if len(sizes) == 0 {
		t.Fatalf("NT_EVAL_SIZES %q had no usable sizes", raw)
	}
	return sizes
}

// TestRealStoreHitAtOne measures nt recall's HIT@1 against a REAL on-disk nt
// store at increasing corpus sizes, to settle roadmap item 13: does accuracy
// actually degrade as a store grows? It is a measurement harness, not a
// correctness test — by default it reports via t.Logf and only fails on
// operational errors (unreadable store, malformed corpus, a target note
// missing from the store). Set NT_EVAL_MIN_HIT1 to also turn it into a floor
// check on the full-store HIT@1 count.
//
// Both env vars point OUTSIDE this repo on purpose: the store holds a
// user's real notes, and the corpus references them by id/title, so neither
// may ever be committed here.
//
//	NT_EVAL_STORE  - path to an nt store directory (the one with notes/ in it)
//	NT_EVAL_CORPUS - path to a JSON file matching the evalQuery format above:
//	                 one row per real query a past session would plausibly
//	                 ask, pointing at the one note that should rank #1.
//
// Run it with:
//
//	NT_EVAL_STORE=~/.local/share/nt NT_EVAL_CORPUS=./eval-corpus.json \
//	    go test ./internal/recall/ -run RealStore -v
//
// Also set NT_EVAL_SIZES (comma-separated) to override the corpus sizes;
// see evalSizes.
//
// Optional regression gate — set both NT_EVAL_STORE/NT_EVAL_CORPUS above
// AND this; the 22-query corpus caps HIT@1 at 22, so keep the floor <= 22:
//
//	NT_EVAL_MIN_HIT1=12 go test ./internal/recall/ -run RealStore -v
func TestRealStoreHitAtOne(t *testing.T) {
	storeDir := os.Getenv("NT_EVAL_STORE")
	corpusPath := os.Getenv("NT_EVAL_CORPUS")
	if storeDir == "" || corpusPath == "" {
		t.Skip("set NT_EVAL_STORE (a store dir) and NT_EVAL_CORPUS (a JSON query file, see doc comment) to run this eval")
	}

	s := &store.Store{Dir: storeDir}
	if fi, err := os.Stat(s.NotesDir()); err != nil || !fi.IsDir() {
		t.Fatalf("NT_EVAL_STORE %s has no readable notes/ directory: %v", storeDir, err)
	}
	_ = LoadUserSynonyms(storeDir) // best-effort, matching cmdRecall
	notes, err := note.List(s)
	if err != nil {
		t.Fatalf("note.List(%s): %v", storeDir, err)
	}
	notes = note.Active(notes) // drop archived/superseded, matching what recall sees in practice
	kept := notes[:0]
	for _, n := range notes {
		if !n.Reserved() { // __tasks__/ notes: RankProject skips them as candidates, so they
			kept = append(kept, n) // must not inflate corpus size or IDF's n either
		}
	}
	notes = kept

	raw, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatalf("reading corpus %s: %v", corpusPath, err)
	}
	var queries []evalQuery
	if err := json.Unmarshal(raw, &queries); err != nil {
		t.Fatalf("malformed corpus %s: %v", corpusPath, err)
	}
	if len(queries) == 0 {
		t.Fatalf("corpus %s has no queries", corpusPath)
	}

	// find resolves a corpus row to the one *note.Note it means: by ID when
	// the row supplies one (stable across renames), else by first Title
	// match. nt permits duplicate titles, so a title-only row could in
	// principle resolve to the wrong note of several sharing that title;
	// the resolved note is threaded through (see `resolved` below) so the
	// later hit check compares Rel identity, not title text again.
	find := func(id, title string) *note.Note {
		for _, n := range notes {
			if id != "" {
				if n.ID == id {
					return n
				}
				continue
			}
			if n.Title == title {
				return n
			}
		}
		return nil
	}

	targetSet := map[string]*note.Note{}         // keyed by Rel, dedups a target shared by several queries
	resolved := make([]*note.Note, len(queries)) // per-query resolved target, so the hit check
	for i, q := range queries {                  // compares Rel, not a possibly-duplicated title
		n := find(q.TargetID, q.TargetTitle)
		if n == nil {
			t.Fatalf("target note not found in store: id=%q title=%q (query %q)", q.TargetID, q.TargetTitle, q.Query)
		}
		targetSet[n.Rel] = n
		resolved[i] = n
	}
	var targets []*note.Note
	for _, n := range targetSet {
		targets = append(targets, n)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Rel < targets[j].Rel })

	var others []*note.Note
	for _, n := range notes {
		if _, isTarget := targetSet[n.Rel]; !isTarget {
			others = append(others, n)
		}
	}
	// note.List already returns others sorted by Rel, so shuffling below
	// starts from a deterministic order rather than map/filesystem order.
	shuffled := append([]*note.Note{}, others...)
	rng := rand.New(rand.NewSource(1)) // fixed seed: a flaky eval is worthless
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	type row struct{ size, hits, empties, total int }
	var results []row
	fullHits, fullTotal := -1, -1

	for _, size := range evalSizes(t) {
		var corpus []*note.Note
		if size == 0 {
			corpus = notes
		} else {
			if size < len(targets) {
				t.Logf("skip size %d: %d target notes don't fit in a corpus that small", size, len(targets))
				continue
			}
			corpus = append(corpus, targets...)
			need := size - len(targets)
			if need > len(shuffled) {
				need = len(shuffled) // store doesn't have enough distractors; use what's there
			}
			corpus = append(corpus, shuffled[:need]...)
		}

		hits, empties := 0, 0
		for i, q := range queries {
			// expiredPenalty (recall.go) reads time.Now(), so this is a
			// point-in-time measurement: a note nearing its valid_until can
			// score differently on a re-run days later.
			//
			// Rank, not RankProject: the CLI's project/NT_WORKSTREAM boost is
			// deliberately left off here to isolate lexical scoring from a
			// per-caller preference this corpus has no ground truth for.
			ranked := Rank(corpus, q.Query, 1)
			if len(ranked) == 0 {
				empties++ // returned nothing — distinct failure mode from returning the wrong note
				continue
			}
			// Compare identity (Rel) against the resolved target note, not
			// top.ID == q.TargetID or top.Title == q.TargetTitle directly:
			// nt permits duplicate titles, so two different notes can share
			// q.TargetTitle, and only the resolved note is the real target.
			if ranked[0].Note.Rel == resolved[i].Rel {
				hits++
			}
		}
		results = append(results, row{size, hits, empties, len(queries)})
		if size == 0 {
			fullHits, fullTotal = hits, len(queries)
		}
	}

	for _, r := range results {
		label := fmt.Sprintf("%d", r.size)
		switch {
		case r.size == 0:
			label = fmt.Sprintf("%d (whole store)", len(notes))
		case r.size == len(targets):
			label = fmt.Sprintf("%d (targets only, no distractors)", r.size)
		}
		t.Logf("corpus size %-32s HIT@1 %d/%d (%.1f%%), empty %d", label, r.hits, r.total, 100*float64(r.hits)/float64(r.total), r.empties)
	}

	floor := os.Getenv("NT_EVAL_MIN_HIT1")
	if floor == "" {
		return
	}
	minHit1, err := strconv.Atoi(floor)
	if err != nil {
		t.Fatalf("NT_EVAL_MIN_HIT1=%q is not an integer", floor)
	}
	if fullTotal < 0 {
		t.Fatalf("NT_EVAL_MIN_HIT1 set but the full-corpus size was skipped (see log)")
	}
	if fullHits < minHit1 {
		t.Errorf("HIT@1 at full corpus size is %d/%d, below the NT_EVAL_MIN_HIT1 floor of %d", fullHits, fullTotal, minHit1)
	}
}
