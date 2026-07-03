package note

import (
	"path"
	"sort"
	"strings"
	"time"
)

// The index's tiering knobs. The catalog used to print one stub per note
// forever (~60 tokens each), so a session-start `nt index` grew linearly with
// store HISTORY; tiering bounds it by store CONVENTIONS (pinned) + recent
// ACTIVITY instead. Small stores are never tiered — completeness is cheap there.
const (
	// TierSmallStore is the note count at or below which the index shows
	// everything untired (today's behavior).
	TierSmallStore = 30
	// TierRecentDays is the recency window for the middle tier.
	TierRecentDays = 14
	// TierRecentCap bounds the recent tier even during a busy fortnight.
	TierRecentCap = 50
	// TierPinnedWarn is the pinned-tier size doctor warns at — "always shown"
	// invites dumping, so the cost is made legible rather than forbidden.
	TierPinnedWarn = 15
)

// pinnedFolders are the store layers whose notes are standing knowledge —
// rules, core memory, and reference maps — consulted at every session start
// regardless of age. They match the conventions the OpenCode integration
// seeds (rules/+rule, memory/+memory-core) plus ref/ and an explicit pin tag.
var pinnedFolders = []string{"rules/", "memory/", "ref/"}
var pinnedTags = map[string]bool{"rule": true, "memory-core": true, "ref": true, "pin": true}

// Pinned reports whether a note belongs to the always-shown index tier:
// standing knowledge whose relevance does not decay with file age.
func (n *Note) Pinned() bool {
	for _, t := range n.Tags {
		if pinnedTags[t] {
			return true
		}
	}
	for _, f := range pinnedFolders {
		if strings.HasPrefix(n.Rel, f) {
			return true
		}
	}
	return false
}

// ChangedDate is the note's effective change date (YYYY-MM-DD): the later of
// its file mtime (catches external edits) and its frontmatter updated/created.
func (n *Note) ChangedDate() string {
	d := n.Updated
	if d == "" {
		d = n.Created
	}
	if len(d) >= 10 {
		d = d[:10]
	}
	if !n.ModTime.IsZero() {
		if m := n.ModTime.Format("2006-01-02"); m > d {
			d = m
		}
	}
	return d
}

// Tiers is the index default view of a large store: standing notes always,
// recent activity in full, and the long tail as per-folder counts (each with
// the expansion path printed by the caller — nothing hides silently).
type Tiers struct {
	Pinned        []*Note
	Recent        []*Note
	OlderByFolder map[string]int
	OlderTotal    int
	Tiered        bool // false ⇒ small store, everything is in Recent
}

// TierIndex splits an already-filtered index note set into tiers as of `now`.
// Order within tiers: Pinned by folder/title (stable shape), Recent newest
// first (what changed since last session reads top-down).
func TierIndex(notes []*Note, now time.Time) Tiers {
	if len(notes) <= TierSmallStore {
		return Tiers{Recent: notes}
	}
	cutoff := now.AddDate(0, 0, -TierRecentDays).Format("2006-01-02")
	t := Tiers{OlderByFolder: map[string]int{}, Tiered: true}
	var recent []*Note
	for _, n := range notes {
		switch {
		case n.Pinned():
			t.Pinned = append(t.Pinned, n)
		case n.ChangedDate() >= cutoff:
			recent = append(recent, n)
		default:
			t.OlderByFolder[folderOf(n)]++
			t.OlderTotal++
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].ChangedDate() > recent[j].ChangedDate() })
	if len(recent) > TierRecentCap {
		for _, n := range recent[TierRecentCap:] {
			t.OlderByFolder[folderOf(n)]++
			t.OlderTotal++
		}
		recent = recent[:TierRecentCap]
	}
	t.Recent = recent
	return t
}

// folderOf is the rollup key: the note's top-level folder under notes/.
func folderOf(n *Note) string {
	d := path.Dir(n.Rel)
	if d == "." {
		return ""
	}
	if i := strings.IndexByte(d, '/'); i >= 0 {
		return d[:i]
	}
	return d
}
