package recall

import (
	"fmt"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// Tied results must come back in the same order on every invocation — agents
// re-run recall constantly, and an unstable --limit cutoff on homogeneous
// stores made "identical" calls return different subsets.
func TestRankDeterministicOrder(t *testing.T) {
	var notes []*note.Note
	for i := 0; i < 12; i++ {
		notes = append(notes, &note.Note{
			Title: fmt.Sprintf("cache invalidation decision %d", i),
			Tags:  []string{"decision"},
			Rel:   fmt.Sprintf("decisions/n-%02d.md", i),
			Body:  "cache invalidation details",
		})
	}
	first := Rank(notes, "improving the cache invalidation retry layer approach", 8)
	for run := 0; run < 25; run++ {
		got := Rank(notes, "improving the cache invalidation retry layer approach", 8)
		if len(got) != len(first) {
			t.Fatalf("run %d: result count changed: %d vs %d", run, len(got), len(first))
		}
		for i := range got {
			if got[i].Note.Rel != first[i].Note.Rel {
				t.Fatalf("run %d: order changed at %d: %s vs %s", run, i, got[i].Note.Rel, first[i].Note.Rel)
			}
		}
	}
}
