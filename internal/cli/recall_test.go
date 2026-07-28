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

// Every scored plain-text row carries a [tier m/n] confidence suffix; the
// bare `--lessons-only` enumeration (no query, no scoring) carries none.
func TestRecallPlainTextConfidenceSuffix(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Never log tokens", "--lesson", "--description", "auth leak", "--source", "opencode")

	out := captureRun(t, "recall", "working on jwt auth tokens")
	if !strings.Contains(out, "[strong") && !strings.Contains(out, "[medium") && !strings.Contains(out, "[weak") {
		t.Errorf("scored result missing a tier suffix: %s", out)
	}

	bare := captureRun(t, "recall", "--lessons-only")
	if strings.Contains(bare, "[strong") || strings.Contains(bare, "[medium") || strings.Contains(bare, "[weak") {
		t.Errorf("bare --lessons-only enumeration should carry no tier suffix (no query was scored): %s", bare)
	}
}

// JSON rows gain confidence/tier/coverage without disturbing the existing
// fields an integration already parses (id/title/score/lesson/...).
func TestRecallJSONConfidenceFields(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Never log tokens", "--lesson", "--description", "auth leak", "--source", "opencode")

	out := captureRun(t, "recall", "working on jwt auth tokens", "--json")
	var res []struct {
		ID         string  `json:"id"`
		Title      string  `json:"title"`
		Score      int     `json:"score"`
		Confidence float64 `json:"confidence"`
		Tier       string  `json:"tier"`
		Coverage   string  `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("recall --json invalid: %v\n%s", err, out)
	}
	if len(res) == 0 || res[0].Tier == "" || res[0].Coverage == "" {
		t.Fatalf("want tier/coverage on the JSON row, got %+v", res)
	}
}

// --explain must reject a bare --lessons-only call (nothing to explain), and
// must print a term-by-term trace for a scored query, including the query
// line, per-term hit kinds, and — when a candidate is dropped — a reason.
func TestRecallExplain(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	if _, code := runWithStdout("recall", "--lessons-only", "--explain"); code == 0 {
		t.Fatalf("--explain with no query should be a usage error")
	}
	captureRun(t, "note", "Goroutine deadlock on shared client", "--lesson",
		"--description", "hold no mutex across a channel send", "--source", "opencode")

	out := captureRun(t, "recall", "adding parallel async workers", "--explain")
	if !strings.Contains(out, "query:") {
		t.Errorf("--explain output missing the query line: %s", out)
	}
	if !strings.Contains(out, "Goroutine deadlock") {
		t.Errorf("--explain output missing the result block: %s", out)
	}
	if !strings.Contains(out, "idf") {
		t.Errorf("--explain output missing per-term contributions: %s", out)
	}
}

// --explain-note traces ONE note whether or not it scored — the case a
// list-level --explain can never surface, since a zero-overlap note never
// becomes a candidate at all.
func TestRecallExplainNoteZeroOverlap(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "CSS flexbox overflow fix", "--body", "set min-width:0 on flex items", "--tag", "css", "--source", "opencode")

	out := captureRun(t, "recall", "postgres migration lock_timeout", "--explain-note", "CSS flexbox overflow fix")
	if !strings.Contains(out, "score 0 — never a candidate") {
		t.Errorf("--explain-note on a zero-overlap note should say so: %s", out)
	}
	if !strings.Contains(out, "strong-bag terms") {
		t.Errorf("--explain-note should dump the note's strong-bag vocabulary: %s", out)
	}

	if _, code := runWithStdout("recall", "postgres migration lock_timeout", "--explain-note", "no-such-note-at-all"); code == 0 {
		t.Fatalf("--explain-note on an unresolvable handle should fail")
	}
}

// When even the top hit is weak, plain output leads with a banner — the
// field-report failure ("recall printed something, so it must be relevant")
// this whole feature exists to kill.
func TestRecallWeakTopHitPrintsBanner(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	// Shares exactly ONE concept ("mode", body-only) with a 6-word query — an
	// explicit unrelated description keeps "mode" out of the strong bag
	// (Description() falls back to the body otherwise). The precision floor
	// has nothing else to prefer, so this stays the sole, weakly-confident
	// result.
	captureRun(t, "note", "Weekly planning notes", "--description", "board review",
		"--body", "review the mode of operation for standup", "--source", "opencode")

	out := captureRun(t, "recall", "how to configure tailwind dark mode responsively")
	if !strings.HasPrefix(out, "~ all matches weak") {
		t.Fatalf("want the weak-top-hit banner as the first line, got:\n%s", out)
	}
	if !strings.Contains(out, "[weak") {
		t.Errorf("want a [weak m/n] suffix on the result row: %s", out)
	}
}
