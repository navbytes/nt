package recall

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

func mk(title, body string, tags ...string) *note.Note {
	return &note.Note{Title: title, Body: body, Tags: tags, Rel: strings.ToLower(strings.ReplaceAll(title, " ", "-")) + ".md"}
}

// The core claim: recall surfaces a lesson from a PARAPHRASED context that
// substring-AND search would miss (no shared verbatim term).
func TestRankParaphraseRecall(t *testing.T) {
	notes := []*note.Note{
		mk("Goroutine deadlock on shared client", "A mutex was held across a channel send.", "lesson", "concurrency"),
		mk("Deploy needs the --confirm flag", "Production rollout is a no-op without it.", "lesson", "deploy"),
		mk("Grocery list", "milk and eggs", "personal"),
	}
	cases := []struct {
		context   string
		wantTitle string
	}{
		{"adding parallel request handling with async workers", "Goroutine deadlock on shared client"},
		{"how do I release to prod safely", "Deploy needs the --confirm flag"},
	}
	for _, c := range cases {
		got := Rank(notes, c.context, 5)
		if len(got) == 0 || got[0].Note.Title != c.wantTitle {
			t.Errorf("context %q: want top %q, got %v", c.context, c.wantTitle, titles(got))
		}
	}
}

// Lesson notes outrank an equally-relevant reference note.
func TestRankLessonBoost(t *testing.T) {
	notes := []*note.Note{
		mk("Auth reference", "JWT tokens and sessions.", "auth"),
		mk("Never log JWTs", "A past incident leaked tokens to stdout.", "lesson", "auth"),
	}
	got := Rank(notes, "working on jwt token auth", 5)
	if len(got) < 2 || !got[0].Lesson {
		t.Fatalf("lesson note should rank first, got %v", titles(got))
	}
}

// Irrelevant notes are dropped; an unrelated context returns nothing.
func TestRankDropsNoise(t *testing.T) {
	notes := []*note.Note{mk("Goroutine deadlock", "mutex", "lesson", "concurrency")}
	if got := Rank(notes, "buy groceries for dinner", 5); len(got) != 0 {
		t.Errorf("unrelated context should return nothing, got %v", titles(got))
	}
}

func titles(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Note.Title
	}
	return out
}
