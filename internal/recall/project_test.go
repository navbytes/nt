package recall

import (
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// The same-project boost tilts ties toward the caller's project without
// filtering or burying genuinely more-relevant foreign notes.
func TestRankProjectBoost(t *testing.T) {
	mine := &note.Note{Title: "cache invalidation strategy", Tags: []string{"gamma", "decision"}, Rel: "decisions/gamma-cache.md", Body: "gamma cache design"}
	theirs := &note.Note{Title: "cache invalidation strategy for alpha", Tags: []string{"alpha", "decision"}, Rel: "decisions/alpha-cache.md", Body: "alpha cache design"}
	notes := []*note.Note{theirs, mine}

	// Equal-ish relevance: the project hint breaks the tie in gamma's favor.
	res := RankProject(notes, "improving the cache invalidation layer", 8, "gamma")
	if len(res) < 2 || res[0].Note != mine || !res[0].ProjectMatch {
		t.Fatalf("project note should rank first with ProjectMatch, got %+v", res)
	}
	if res[1].Note != theirs || res[1].ProjectMatch {
		t.Fatalf("foreign note stays visible below, unmarked: %+v", res[1])
	}

	// Workstream-style hints tokenize: "feat-gamma-cache" still matches @gamma.
	res = RankProject(notes, "improving the cache invalidation layer", 8, "feat-gamma-cache")
	if res[0].Note != mine {
		t.Fatal("tokenized workstream hint should match the project tag")
	}

	// A clearly more-relevant foreign note is NOT buried by the boost.
	strongForeign := &note.Note{Title: "cache invalidation strategy: full postmortem", Tags: []string{"alpha", "lesson"},
		Rel: "lessons/alpha-cache-postmortem.md", Body: "cache invalidation strategy layer improving postmortem details"}
	res = RankProject([]*note.Note{strongForeign, mine}, "improving the cache invalidation layer strategy postmortem", 8, "gamma")
	if res[0].Note != strongForeign {
		t.Fatalf("a much more relevant foreign note must still win, got %q first", res[0].Note.Title)
	}

	// Empty hint = plain Rank (no marks).
	for _, r := range RankProject(notes, "cache invalidation", 8, "") {
		if r.ProjectMatch {
			t.Fatal("no hint must mean no project marks")
		}
	}
}
