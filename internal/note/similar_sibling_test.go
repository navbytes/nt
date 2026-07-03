package note

import "testing"

// Multi-project stores hold same-shaped notes per project ("taskly repo map" /
// "ratelim repo map") — the dedup guard must not flag those as duplicates, while
// still catching a genuine fork of one decision. Field-study regression.
func TestFindSimilarParallelSiblings(t *testing.T) {
	existing := &Note{Title: "taskly repo map and test workflow", Tags: []string{"taskly", "ref"}, Rel: "ref/taskly-repo-map-and-test-workflow.md"}

	// Same shape, different project, each self-identified by its project tag in
	// the title → NOT similar.
	if sim := FindSimilar([]*Note{existing}, "ratelim repo map and test workflow", []string{"ratelim", "ref"}); len(sim) != 0 {
		t.Fatalf("parallel project siblings flagged as duplicates: %v", sim[0].Title)
	}
	// A true fork (same topic, same tags) is still caught.
	if sim := FindSimilar([]*Note{existing}, "taskly repo map and workflow for tests", []string{"taskly", "ref"}); len(sim) != 1 {
		t.Fatalf("genuine near-duplicate not caught")
	}
}
