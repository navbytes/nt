package note

import "testing"

func TestNearDupPairsExactSlugMatch(t *testing.T) {
	a := &Note{ID: "A", Title: "Token storage in httpOnly cookie", Rel: "a.md"}
	b := &Note{ID: "B", Title: "Token storage in HttpOnly Cookie", Rel: "b.md"}
	pairs := NearDupPairs([]*Note{a, b})
	if len(pairs) != 1 {
		t.Fatalf("want 1 pair, got %d", len(pairs))
	}
	ids := map[string]bool{pairs[0].A.ID: true, pairs[0].B.ID: true}
	if !ids["A"] || !ids["B"] {
		t.Errorf("unexpected pair: %+v", pairs[0])
	}
}

func TestNearDupPairsSharedTagAndTitleOverlap(t *testing.T) {
	a := &Note{ID: "A", Title: "Retry backoff strategy", Rel: "a.md", Tags: []string{"resilience"}}
	b := &Note{ID: "B", Title: "Retry Backoff Strategy", Rel: "b.md", Tags: []string{"resilience"}}
	pairs := NearDupPairs([]*Note{a, b})
	if len(pairs) != 1 {
		t.Fatalf("want 1 pair, got %d", len(pairs))
	}
}

func TestNearDupPairsUnrelatedNotesExcluded(t *testing.T) {
	a := &Note{ID: "A", Title: "Cache invalidation strategy", Rel: "a.md"}
	b := &Note{ID: "B", Title: "Grocery list", Rel: "b.md"}
	pairs := NearDupPairs([]*Note{a, b})
	if len(pairs) != 0 {
		t.Fatalf("want 0 pairs for unrelated notes, got %d: %+v", len(pairs), pairs)
	}
}

func TestNearDupPairsDistinctTagExcludes(t *testing.T) {
	a := &Note{ID: "A", Title: "Retry backoff strategy", Rel: "a.md", Tags: []string{"distinct"}}
	b := &Note{ID: "B", Title: "Retry Backoff Strategy", Rel: "b.md"}
	pairs := NearDupPairs([]*Note{a, b})
	if len(pairs) != 0 {
		t.Fatalf("a distinct-tagged note should exclude the pair, got %d", len(pairs))
	}
}

func TestNearDupPairsSkipsReservedNotes(t *testing.T) {
	a := &Note{ID: "A", Title: "Fix the login bug", Rel: "__tasks__/a.md"}
	b := &Note{ID: "B", Title: "Fix the login bug", Rel: "__tasks__/b.md"}
	pairs := NearDupPairs([]*Note{a, b})
	if len(pairs) != 0 {
		t.Fatalf("machine task-detail notes should be excluded, got %d", len(pairs))
	}
}
