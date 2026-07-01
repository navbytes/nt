package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// `nt note --lesson` files under lessons/ and tags `lesson`; `nt recall` then
// surfaces it from a PARAPHRASED context that `nt search` (substring) misses.
func TestRecallSurfacesLessonFromParaphrase(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Goroutine deadlock on shared client", "--lesson",
		"--description", "hold no mutex across a channel send", "--source", "opencode")

	// Lesson conventions applied.
	lessons := captureRun(t, "notes", "--folder", "lessons")
	if !strings.Contains(lessons, "Goroutine deadlock") {
		t.Fatalf("--lesson should file under lessons/: %s", lessons)
	}

	// recall finds it from words that don't appear in the note verbatim.
	out := captureRun(t, "recall", "adding", "a", "parallel", "async", "worker")
	if !strings.Contains(out, "Goroutine deadlock") {
		t.Errorf("recall should surface the lesson from a paraphrase:\n%s", out)
	}
	// search (substring-AND) does not.
	if s := captureRun(t, "search", "parallel async worker"); strings.Contains(s, "Goroutine deadlock") {
		t.Errorf("substring search unexpectedly matched the paraphrase:\n%s", s)
	}
}

func TestRecallJSONAndLessonsOnly(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Never log tokens", "--lesson", "--description", "auth leak", "--source", "opencode")
	captureRun(t, "note", "Auth reference", "--body", "jwt sessions", "--tag", "auth", "--folder", "ref", "--source", "opencode")

	out := captureRun(t, "recall", "working on jwt auth tokens", "--json")
	var res []struct {
		Title  string `json:"title"`
		Lesson bool   `json:"lesson"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("recall --json invalid: %v\n%s", err, out)
	}
	if len(res) < 2 || !res[0].Lesson {
		t.Errorf("lesson should rank first, got %v", res)
	}

	only := captureRun(t, "recall", "working on jwt auth tokens", "--lessons-only", "--json")
	json.Unmarshal([]byte(only), &res)
	for _, r := range res {
		if !r.Lesson {
			t.Errorf("--lessons-only returned a non-lesson: %s", r.Title)
		}
	}
}
