package recall

import (
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// Field-study fix: a specific query must not surface a lesson that shares only
// ONE concept with it (the lesson boost was promoting exactly those), and the
// weak tail must be trimmed instead of padding every recall to the limit.
func TestRankPrecisionFloor(t *testing.T) {
	undoLesson := &note.Note{
		Title: "nt undo is store-global and reverts other agents' operations",
		Tags:  []string{"lesson", "nt"},
		Body:  "Running undo reverted another agent's done in a different workstream.",
	}
	target := &note.Note{
		Title: "git workflow for the release branch",
		Tags:  []string{"ref"},
		Body:  "To revert a bad git commit on the release branch use git revert, never reset --hard.",
	}
	notes := []*note.Note{undoLesson, target}

	// Adjacent-but-different topic: shares only "revert" with the undo lesson.
	res := Rank(notes, "revert a bad git commit on the release branch", 8)
	for i, r := range res {
		if r.Note == undoLesson && i < 1 {
			t.Fatalf("single-concept lesson ranked #%d for an unrelated query", i+1)
		}
	}
	if len(res) == 0 || res[0].Note != target {
		t.Fatalf("the genuinely relevant note should rank first, got %+v", res)
	}

	// Multi-concept paraphrase still reaches the lesson (sensitivity retained).
	res = Rank(notes, "my rollback silently reopened a teammate's completed work in another workstream", 8)
	if len(res) == 0 || res[0].Note != undoLesson {
		t.Fatalf("paraphrase should still surface the lesson first")
	}
}
