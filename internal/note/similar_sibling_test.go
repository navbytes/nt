package note

import "testing"

// Multi-project stores hold same-shaped notes per project ("taskly repo map" /
// "ratelim repo map") — the dedup guard must not flag those as duplicates, while
// still catching a genuine fork of one decision. Field-study regression.
func TestFindSimilarParallelSiblings(t *testing.T) {
	existing := &Note{Title: "taskly repo map and test workflow", Tags: []string{"taskly", "ref"}, Rel: "ref/taskly-repo-map-and-test-workflow.md"}

	// Same shape, different project, each self-identified by its project tag in
	// the title → NOT similar.
	if sim := FindSimilar([]*Note{existing}, "ratelim repo map and test workflow", []string{"ratelim", "ref"}, ""); len(sim) != 0 {
		t.Fatalf("parallel project siblings flagged as duplicates: %v", sim[0].Title)
	}
	// A true fork (same topic, same tags) is still caught.
	if sim := FindSimilar([]*Note{existing}, "taskly repo map and workflow for tests", []string{"taskly", "ref"}, ""); len(sim) != 1 {
		t.Fatalf("genuine near-duplicate not caught")
	}
}

// TestFindSimilarProjectSharedTag is the regression for the dead-gate bug:
// `--project` stores its value as a `project:` frontmatter field, not a tag, so
// a note whose only tag is a class marker (lesson/rule/memory-core — stripped
// by structuralTag) had an EMPTY tag set and the Jaccard branch could never
// fire — only an exact-slug match could. Two same-project lesson notes with
// heavily overlapping titles must now pair.
func TestFindSimilarProjectSharedTag(t *testing.T) {
	existing := &Note{
		Title: "postgres connection pool exhaustion",
		Tags:  []string{"lesson"},
		Extra: []string{"project: wtcockpit"},
		Rel:   "lessons/postgres-connection-pool-exhaustion.md",
	}
	sim := FindSimilar([]*Note{existing}, "postgres connection pool exhaustion issue",
		[]string{"lesson"}, "wtcockpit")
	if len(sim) != 1 {
		t.Fatalf("same-project lesson near-duplicate not caught (tag set dead-gated by structural-only tags): got %d", len(sim))
	}
}

// TestFindSimilarProjectParallelSiblings proves the project-as-tag fix doesn't
// regress the multi-project protection: two notes that share a real tag but
// belong to different projects, each self-identified by its project appearing
// in its own title, are the same "parallel sibling" shape as the tag-driven
// case in TestFindSimilarParallelSiblings — just distinguished via `project:`
// instead of a tag.
func TestFindSimilarProjectParallelSiblings(t *testing.T) {
	existing := &Note{
		Title: "taskly repo map and test workflow",
		Tags:  []string{"ref"},
		Extra: []string{"project: taskly"},
		Rel:   "ref/taskly-repo-map-and-test-workflow.md",
	}
	sim := FindSimilar([]*Note{existing}, "ratelim repo map and test workflow",
		[]string{"ref"}, "ratelim")
	if len(sim) != 0 {
		t.Fatalf("per-project variants flagged as duplicates: %v", sim[0].Title)
	}
}

// TestFindSimilarStructuralOnlyNoProject is the main regression risk of the
// project fix: without --project, two notes whose only tag is a class marker
// must NOT suddenly look topically related just because they're both lessons.
// Their titles overlap heavily but they share nothing once "lesson" is
// stripped and neither carries a project — same dead (correctly so) tag set as
// before the fix.
func TestFindSimilarStructuralOnlyNoProject(t *testing.T) {
	existing := &Note{
		Title: "postgres connection pool exhaustion",
		Tags:  []string{"lesson"},
		Rel:   "lessons/postgres-connection-pool-exhaustion.md",
	}
	sim := FindSimilar([]*Note{existing}, "postgres connection pool exhaustion issue",
		[]string{"lesson"}, "")
	if len(sim) != 0 {
		t.Fatalf("structural-only lessons with no project were flagged as near-duplicates: %v", sim)
	}
}

// TestFindSimilarSameProjectDifferentTopics is the other main regression risk
// of the project fix: folding project into the tag set must not turn a shared
// project into a blanket topic match. Two lessons in the same project but
// about unrelated things share the project "tag" but nothing in title
// Jaccard, so they must not pair — otherwise every lesson in a project would
// nuisance-pair with every other, training users to reach for --force.
func TestFindSimilarSameProjectDifferentTopics(t *testing.T) {
	existing := &Note{
		Title: "postgres connection pool exhaustion",
		Tags:  []string{"lesson"},
		Extra: []string{"project: wtcockpit"},
		Rel:   "lessons/postgres-connection-pool-exhaustion.md",
	}
	sim := FindSimilar([]*Note{existing}, "vitest mocks leak between files",
		[]string{"lesson"}, "wtcockpit")
	if len(sim) != 0 {
		t.Fatalf("same-project, different-topic notes flagged as near-duplicates: %v", sim)
	}
}

// TestFindSimilarProjectCaseWhitespaceNormalized pins the fold-point
// normalization: project is compared trimmed and lowercased, so an agent
// writing "wtc" one turn and " WTC " (or "WTC") the next still gets caught by
// the dedup guard instead of silently reverting to the pre-fix dead-gate
// behaviour. TAG case-sensitivity is unrelated and untouched.
func TestFindSimilarProjectCaseWhitespaceNormalized(t *testing.T) {
	existing := &Note{
		Title: "postgres connection pool exhaustion",
		Tags:  []string{"lesson"},
		Extra: []string{"project: wtc"},
		Rel:   "lessons/postgres-connection-pool-exhaustion.md",
	}
	if sim := FindSimilar([]*Note{existing}, "postgres connection pool exhaustion issue",
		[]string{"lesson"}, "WTC"); len(sim) != 1 {
		t.Fatalf("case-differing project not folded together: got %d", len(sim))
	}
	if sim := FindSimilar([]*Note{existing}, "postgres connection pool exhaustion issue",
		[]string{"lesson"}, "  wtc  "); len(sim) != 1 {
		t.Fatalf("whitespace-differing project not folded together: got %d", len(sim))
	}
}
