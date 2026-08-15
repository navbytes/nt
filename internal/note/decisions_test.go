package note

import (
	"strings"
	"testing"
)

func TestAppendDecisionCreatesSection(t *testing.T) {
	n := &Note{Body: "# T\n\nSome content.\n"}
	if err := AppendDecision(n, "2026-07-30", "switched X to Y — Z"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, DecisionsHeading+"\n\n- 2026-07-30: switched X to Y — Z\n") {
		t.Fatalf("section not created correctly:\n%s", n.Body)
	}
	count, latest := n.DecisionStats()
	if count != 1 || latest != "2026-07-30" {
		t.Fatalf("stats = (%d,%q)", count, latest)
	}
}

func TestAppendDecisionPrependsNewestFirst(t *testing.T) {
	n := &Note{Body: "content\n\n## Decisions\n\n- 2026-06-01: chose JWT\n"}
	if err := AppendDecision(n, "2026-07-30", "raised refresh to 30d"); err != nil {
		t.Fatal(err)
	}
	i1 := strings.Index(n.Body, "2026-07-30")
	i2 := strings.Index(n.Body, "2026-06-01")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Fatalf("newest should come first:\n%s", n.Body)
	}
	count, latest := n.DecisionStats()
	if count != 2 || latest != "2026-07-30" {
		t.Fatalf("stats = (%d,%q)", count, latest)
	}
}

func TestAppendDecisionHostileInput(t *testing.T) {
	n := &Note{Body: "x\n"}
	for _, bad := range []string{"", "two\nlines", "---", "# heading smuggle"} {
		if err := AppendDecision(n, "2026-07-30", bad); err == nil {
			t.Errorf("should refuse %q", bad)
		}
	}
}

func TestAppendDecisionIgnoresFencedHeading(t *testing.T) {
	// A note documenting the convention itself: the example heading lives in a
	// code fence and must not attract real bullets.
	n := &Note{Body: "How the log works:\n\n```markdown\n## Decisions\n- 2026-01-01: example\n```\n"}
	if err := AppendDecision(n, "2026-07-30", "real decision"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "```\n\n"+DecisionsHeading) {
		t.Fatalf("real section should be created OUTSIDE the fence:\n%s", n.Body)
	}
	count, latest := n.DecisionStats()
	if count != 1 || latest != "2026-07-30" {
		t.Fatalf("stats should see only the real section, got (%d,%q)", count, latest)
	}
}

func TestDecisionStatsScopesToSection(t *testing.T) {
	// Dated bullets OUTSIDE the section don't count; a later heading ends it.
	n := &Note{Body: "- 2026-01-01: not a decision\n\n## Decisions\n\n- 2026-05-05: real\n\n## Notes\n\n- 2026-06-06: also not\n"}
	count, latest := n.DecisionStats()
	if count != 1 || latest != "2026-05-05" {
		t.Fatalf("stats = (%d,%q), want (1,2026-05-05)", count, latest)
	}
	// A prose heading that merely starts with the words is not the section.
	n2 := &Note{Body: "## Decisions we regret\n\n- 2026-05-05: nope\n"}
	if c, _ := n2.DecisionStats(); c != 0 {
		t.Fatalf("prose heading must not count, got %d", c)
	}
}
