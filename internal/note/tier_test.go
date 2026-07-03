package note

import (
	"fmt"
	"testing"
	"time"
)

func mk(rel, title string, tags []string, updated string) *Note {
	return &Note{Rel: rel, Title: title, Tags: tags, Updated: updated}
}

func TestTierIndex(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

	// Small store: never tiered, everything shown.
	small := []*Note{mk("a.md", "a", nil, "2020-01-01")}
	if tr := TierIndex(small, now); tr.Tiered || len(tr.Recent) != 1 {
		t.Fatalf("small store must not tier: %+v", tr)
	}

	// Large store: pinned escapes recency; recent windowed; rest rolled up.
	var notes []*Note
	notes = append(notes,
		mk("rules/style.md", "code style rules", []string{"rule"}, "2024-01-01"),          // old but pinned (tag)
		mk("memory/core.md", "user preferences", []string{"memory-core"}, "2024-01-01"),   // pinned
		mk("ref/repo-map.md", "repo map", nil, "2024-01-01"),                              // pinned (folder)
		mk("decisions/pinme.md", "special decision", []string{"pin"}, "2023-01-01"),       // pinned (pin tag)
		mk("decisions/fresh.md", "fresh decision", []string{"decision"}, "2026-07-01"),    // recent
		mk("lessons/fresh-lesson.md", "fresh lesson", []string{"lesson"}, "2026-06-25"),   // recent
	)
	for i := 0; i < 40; i++ { // old tail
		notes = append(notes, mk(fmt.Sprintf("decisions/old-%d.md", i), fmt.Sprintf("old decision %d", i), []string{"decision"}, "2025-01-01"))
	}
	tr := TierIndex(notes, now)
	if !tr.Tiered {
		t.Fatal("46-note store should tier")
	}
	if len(tr.Pinned) != 4 {
		t.Fatalf("want 4 pinned, got %d", len(tr.Pinned))
	}
	if len(tr.Recent) != 2 || tr.Recent[0].Title != "fresh decision" {
		t.Fatalf("recent tier wrong (want newest first): %+v", tr.Recent)
	}
	if tr.OlderTotal != 40 || tr.OlderByFolder["decisions"] != 40 {
		t.Fatalf("rollup wrong: total=%d byFolder=%v", tr.OlderTotal, tr.OlderByFolder)
	}

	// Recent cap: a busy month overflows into the rollup, newest kept.
	var busy []*Note
	busy = append(busy, notes[:4]...) // the pinned four
	for i := 0; i < 80; i++ { // all within the 14d window (cutoff 2026-06-19)
		busy = append(busy, mk(fmt.Sprintf("inbox/n-%d.md", i), fmt.Sprintf("hot note %d", i), nil, fmt.Sprintf("2026-06-%02d", i%9+20)))
	}
	tr = TierIndex(busy, now)
	if len(tr.Recent) != TierRecentCap {
		t.Fatalf("recent tier should cap at %d, got %d", TierRecentCap, len(tr.Recent))
	}
	if tr.OlderTotal == 0 {
		t.Fatal("capped overflow should land in the rollup")
	}
}
