package note

import (
	"path"
	"strings"
)

// FindExact returns the canonical existing note for a title, if there is one:
// the note whose filename slug equals Slug(title), or whose title matches
// case-insensitively. Unlike FindSimilar (fuzzy, advisory), this is the
// deterministic match behind `if_exists` write steering — an agent about to
// create "JWT token lifetime" is pointed at the existing jwt-token-lifetime
// note instead of minting a sibling.
//
// folder, when non-empty, scopes the match to notes filed directly in that
// folder (path-cleaned). Callers pass Active (non-archived, non-superseded)
// notes only: a consolidation decision must not resurrect through this door —
// note.Active does that filtering, and this function additionally skips
// reserved (machine) notes defensively.
func FindExact(notes []*Note, title, folder string) *Note {
	want := Slug(title)
	folder = strings.Trim(path.Clean("/"+strings.TrimSpace(folder)), "/")
	if folder == "." {
		folder = ""
	}
	for _, n := range notes {
		if n.Reserved() {
			continue
		}
		if folder != "" {
			d := path.Dir(n.Rel)
			if d == "." {
				d = ""
			}
			if d != folder {
				continue
			}
		}
		base := strings.TrimSuffix(path.Base(n.Rel), ".md")
		if base == want && want != "" {
			return n
		}
		if strings.EqualFold(strings.TrimSpace(n.Title), strings.TrimSpace(title)) {
			return n
		}
	}
	return nil
}
