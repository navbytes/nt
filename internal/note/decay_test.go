package note

import (
	"testing"
	"time"
)

func TestParseHalfLife(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		in     string
		want   time.Duration
		ok     bool
		isNone bool
	}{
		{"90d", 90 * day, true, false},
		{"12w", 12 * 7 * day, true, false},
		{"6m", 6 * 30 * day, true, false},
		{"2y", 2 * 365 * day, true, false},
		{" 90D ", 90 * day, true, false}, // trimmed + case-insensitive
		{"none", 0, false, true},
		{"", 0, false, false},
		{"0d", 0, false, false},   // zero is nonsense, not "instant fade"
		{"-5d", 0, false, false},  // negative likewise
		{"90", 0, false, false},   // missing unit
		{"d", 0, false, false},    // missing count
		{"90x", 0, false, false},  // unknown unit
		{"9 0d", 0, false, false}, // garbage
	}
	for _, c := range cases {
		d, ok, isNone := ParseHalfLife(c.in)
		if d != c.want || ok != c.ok || isNone != c.isNone {
			t.Errorf("ParseHalfLife(%q) = (%v,%v,%v), want (%v,%v,%v)", c.in, d, ok, isNone, c.want, c.ok, c.isNone)
		}
	}
}

func TestDecayMath(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	n := &Note{HalfLife: "90d", Created: "2026-07-30"}
	if d := n.Decay(now); d < 0.99 {
		t.Fatalf("fresh note should barely decay, got %v", d)
	}
	// One half-life old → 0.5.
	n.Created = now.AddDate(0, 0, -90).Format("2006-01-02")
	if d := n.Decay(now); d < 0.48 || d > 0.52 {
		t.Fatalf("one half-life should decay to ~0.5, got %v", d)
	}
	if !n.Faded(now) {
		t.Fatal("a note past one half-life should be faded")
	}
	// Ancient → floored, never zero.
	n.Created = now.AddDate(-5, 0, 0).Format("2006-01-02")
	if d := n.Decay(now); d != DecayFloor {
		t.Fatalf("ancient note should sit at the floor %v, got %v", DecayFloor, d)
	}
	// reviewed: resets the clock even when created is ancient.
	n.Reviewed = now.AddDate(0, 0, -1).Format("2006-01-02")
	if d := n.Decay(now); d < 0.98 {
		t.Fatalf("a just-reviewed note should not be decayed, got %v", d)
	}
	if n.Faded(now) {
		t.Fatal("a just-reviewed note is not faded")
	}
}

func TestDecayOptIn(t *testing.T) {
	now := time.Now()
	old := now.AddDate(-3, 0, 0).Format("2006-01-02")
	for _, hl := range []string{"", "none", "garbage", "0d"} {
		n := &Note{HalfLife: hl, Created: old}
		if d := n.Decay(now); d != 1.0 {
			t.Errorf("half_life %q must mean no decay, got %v", hl, d)
		}
		if n.Faded(now) {
			t.Errorf("half_life %q must never fade", hl)
		}
	}
	// No parseable age basis → no decay (never punish missing metadata).
	n := &Note{HalfLife: "30d"}
	if d := n.Decay(now); d != 1.0 {
		t.Fatalf("no age basis must mean no decay, got %v", d)
	}
}

func TestHalfLifeReviewedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NT_DIR", dir)
	s := testStore(t)
	n, err := Create(s, "decaying fact", "body", []string{"x"}, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	n.HalfLife = "90d"
	n.Reviewed = "2026-07-30"
	if err := n.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HalfLife != "90d" || got.Reviewed != "2026-07-30" {
		t.Fatalf("round-trip lost decay fields: half_life=%q reviewed=%q", got.HalfLife, got.Reviewed)
	}
}

func TestTierIndexFadedRollsUp(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	recentDate := now.AddDate(0, 0, -2).Format("2006-01-02")
	var notes []*Note
	// Force tiering (> TierSmallStore notes), all file-recent.
	for i := 0; i < TierSmallStore; i++ {
		notes = append(notes, &Note{Rel: "misc/n" + string(rune('a'+i%26)) + ".md", Title: "n", Updated: recentDate})
	}
	fresh := &Note{Rel: "misc/fresh.md", Title: "fresh", Updated: recentDate}
	// Faded: edited recently (file-recent) but reviewed long ago with a short half-life.
	faded := &Note{Rel: "misc/faded.md", Title: "faded", Updated: recentDate,
		HalfLife: "7d", Reviewed: now.AddDate(0, 0, -60).Format("2006-01-02")}
	// A recent Updated: date wins the age basis — so make the faded note's ONLY
	// basis the old reviewed date.
	faded.Updated = ""
	faded.Created = now.AddDate(0, 0, -60).Format("2006-01-02")
	notes = append(notes, fresh, faded)

	tiers := TierIndex(notes, now)
	if !tiers.Tiered {
		t.Fatal("store should be tiered")
	}
	inRecent := func(want *Note) bool {
		for _, n := range tiers.Recent {
			if n == want {
				return true
			}
		}
		return false
	}
	if !inRecent(fresh) {
		t.Fatal("fresh note should be in the recent tier")
	}
	if inRecent(faded) {
		t.Fatal("faded note must roll up out of the recent tier")
	}
	// The arithmetic invariant holds: pinned + recent + older == total.
	if got := len(tiers.Pinned) + len(tiers.Recent) + tiers.OlderTotal; got != len(notes) {
		t.Fatalf("tier arithmetic broke: %d + %d + %d != %d", len(tiers.Pinned), len(tiers.Recent), tiers.OlderTotal, len(notes))
	}
}
