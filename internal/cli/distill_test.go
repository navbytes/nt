package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCmdDistillFindsNearDuplicatePair(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Token storage in httpOnly cookie", "--description", "use httpOnly + secure")
	captureRun(t, "note", "Token storage in HttpOnly Cookie", "--force", "--description", "slightly different casing")
	captureRun(t, "note", "Completely unrelated topic", "--description", "grocery list")

	out := captureRun(t, "distill")
	if !strings.Contains(out, "1 near-duplicate pair") {
		t.Fatalf("expected exactly one near-duplicate pair:\n%s", out)
	}
	if !strings.Contains(out, "Token storage in httpOnly cookie") || !strings.Contains(out, "Token storage in HttpOnly Cookie") {
		t.Fatalf("both near-duplicate titles should appear:\n%s", out)
	}
	if strings.Contains(out, "Completely unrelated topic") {
		t.Fatalf("an unrelated note should not appear in the pairing:\n%s", out)
	}
	if !strings.Contains(out, "nt supersede") || !strings.Contains(out, "nt tag") {
		t.Fatalf("expected merge and keep-both guidance in the output:\n%s", out)
	}
}

func TestCmdDistillJSONShape(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Retry backoff strategy", "--description", "exponential with jitter")
	captureRun(t, "note", "Retry Backoff Strategy", "--force", "--description", "updated numbers")

	out := captureRun(t, "distill", "--json")
	var payload struct {
		Pairs []struct {
			A struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"a"`
			B struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"b"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, out)
	}
	if len(payload.Pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d:\n%s", len(payload.Pairs), out)
	}
	if payload.Pairs[0].A.ID == "" || payload.Pairs[0].B.ID == "" {
		t.Fatalf("pair entries should carry ids:\n%s", out)
	}
}

func TestCmdDistillNoNearDuplicates(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Alpha topic")
	captureRun(t, "note", "Beta topic")

	out := captureRun(t, "distill")
	if !strings.Contains(out, "no near-duplicate notes found") {
		t.Fatalf("expected a clean-store message:\n%s", out)
	}
}

func TestCmdDistillRespectsDistinctTag(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Retry backoff strategy", "--tag", "distinct")
	captureRun(t, "note", "Retry Backoff Strategy", "--force")

	out := captureRun(t, "distill")
	if !strings.Contains(out, "no near-duplicate notes found") {
		t.Fatalf("a pair with a distinct-tagged note should be excluded:\n%s", out)
	}
}

func TestCmdDistillLimit(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Cache invalidation approach")
	captureRun(t, "note", "Cache Invalidation Approach", "--force")
	captureRun(t, "note", "Retry backoff strategy")
	captureRun(t, "note", "Retry Backoff Strategy", "--force")

	out := captureRun(t, "distill", "--json", "--limit", "1")
	var payload struct {
		Pairs     []map[string]any `json:"pairs"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, out)
	}
	if len(payload.Pairs) != 1 {
		t.Fatalf("expected --limit 1 to cap to 1 pair, got %d", len(payload.Pairs))
	}
	if !payload.Truncated {
		t.Fatalf("expected truncated:true when more pairs exist than the limit")
	}
}

// nt distill only ever reads — confirm it never touches the note files it
// reports on.
func TestCmdDistillNeverWrites(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Token storage in httpOnly cookie")
	captureRun(t, "note", "Token storage in HttpOnly Cookie", "--force")

	e, ok := engine()
	if !ok {
		t.Fatal("open engine")
	}
	before := map[string]string{}
	for _, n := range mustNotes(e) {
		body, _ := os.ReadFile(n.Path)
		before[n.Path] = string(body)
	}

	captureRun(t, "distill")

	e2, ok := engine()
	if !ok {
		t.Fatal("open engine")
	}
	for _, n := range mustNotes(e2) {
		body, _ := os.ReadFile(n.Path)
		if string(body) != before[n.Path] {
			t.Errorf("distill modified %s", n.Path)
		}
	}
}
