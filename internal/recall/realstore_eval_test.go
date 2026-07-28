package recall

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
)

// evalQuery is one row of the external JSON corpus read by
// TestRealStoreHitAtOne. The corpus format is fixed:
//
//	[ { "query": "plain-words question a future session would ask",
//	    "targetID": "WV9ZHS",
//	    "targetTitle": "exact title of the note that should rank #1" } ]
type evalQuery struct {
	Query       string `json:"query"`
	TargetID    string `json:"targetID"`
	TargetTitle string `json:"targetTitle"`
}

// evalCorpusSizes are the store sizes at which HIT@1 is measured. 0 means
// "the whole store", whatever size that currently is.
var evalCorpusSizes = []int{10, 20, 40, 0}

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
// Optional regression gate:
//
//	NT_EVAL_MIN_HIT1=30 go test ./internal/recall/ -run RealStore -v
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
	notes, err := note.List(s)
	if err != nil {
		t.Fatalf("note.List(%s): %v", storeDir, err)
	}
	notes = note.Active(notes) // drop archived/superseded, matching what recall sees in practice

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

	// Result (recall.go) has no ID field of its own; it embeds *note.Note,
	// which does carry ID. So a result is matched on Result.Note.ID when the
	// corpus row supplies a targetID (stable across renames), falling back to
	// an exact Result.Note.Title match when it doesn't.
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

	targetSet := map[string]*note.Note{} // keyed by Rel, dedups a target shared by several queries
	for _, q := range queries {
		n := find(q.TargetID, q.TargetTitle)
		if n == nil {
			t.Fatalf("target note not found in store: id=%q title=%q (query %q)", q.TargetID, q.TargetTitle, q.Query)
		}
		targetSet[n.Rel] = n
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

	type row struct{ size, hits, total int }
	var results []row
	fullHits, fullTotal := -1, -1

	for _, size := range evalCorpusSizes {
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

		hits := 0
		for _, q := range queries {
			ranked := Rank(corpus, q.Query, 1)
			if len(ranked) == 0 {
				continue
			}
			top := ranked[0].Note
			if q.TargetID != "" {
				if top.ID == q.TargetID {
					hits++
				}
			} else if top.Title == q.TargetTitle {
				hits++
			}
		}
		results = append(results, row{size, hits, len(queries)})
		if size == 0 {
			fullHits, fullTotal = hits, len(queries)
		}
	}

	for _, r := range results {
		label := fmt.Sprintf("%d", r.size)
		if r.size == 0 {
			label = fmt.Sprintf("%d (whole store)", len(notes))
		}
		t.Logf("corpus size %-16s HIT@1 %d/%d (%.1f%%)", label, r.hits, r.total, 100*float64(r.hits)/float64(r.total))
	}

	floor := os.Getenv("NT_EVAL_MIN_HIT1")
	if floor == "" {
		return
	}
	min, err := strconv.Atoi(floor)
	if err != nil {
		t.Fatalf("NT_EVAL_MIN_HIT1=%q is not an integer", floor)
	}
	if fullTotal < 0 {
		t.Fatalf("NT_EVAL_MIN_HIT1 set but the full-corpus size was skipped (see log)")
	}
	if fullHits < min {
		t.Errorf("HIT@1 at full corpus size is %d/%d, below the NT_EVAL_MIN_HIT1 floor of %d", fullHits, fullTotal, min)
	}
}
