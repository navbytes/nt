package note

// Pair is one near-duplicate-by-title match — shared by `nt doctor`'s
// near-dup lint (CLI), `nt distill`/`nt_distill` (uncapped, full fields), and
// anywhere else two notes need to be flagged as probably-the-same-thing.
type Pair struct{ A, B *Note }

// NearDupWarnThreshold is the pair count at which the catalog surfaces
// (`nt index` / `nt_index`) start warning proactively that the store needs a
// distill pass. Below it, a stray pair is normal working residue and a nag on
// every session start would train readers to ignore the warning; at it, recall
// quality is measurably degrading. Shared by CLI and MCP so both surfaces
// nag (or stay quiet) in unison.
const NearDupWarnThreshold = 3

// HygieneScanMaxNotes caps the store size at which the unscoped index runs
// its proactive hygiene scan: NearDupPairs is O(n²) in active notes, and the
// unscoped index is the hottest read (every session start). Below the cap the
// scan is milliseconds; past it the nudge isn't worth the latency, and doctor
// — on-demand, expected to take a moment — remains the uncapped check. Shared
// by CLI and MCP so both surfaces gate identically.
const HygieneScanMaxNotes = 1500

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// NearDupPairs finds pairs of active, non-reserved (not a machine
// task-detail note) notes with near-duplicate titles — the store rot that
// degrades recall most (title-token overlap + a shared tag, or an
// exact-except-case title; see FindSimilar). A pair where EITHER note
// carries the `distinct` tag is a sanctioned fork (a deliberate --force the
// author already acknowledged) — excluded so a caller doesn't nag forever
// about it. At most one match is reported per note (the first found).
func NearDupPairs(active []*Note) []Pair {
	var out []Pair
	seen := make([]*Note, 0, len(active))
	for _, n := range active {
		if n.Reserved() {
			continue
		}
		if !containsStr(n.Tags, "distinct") {
			if sim := FindSimilar(seen, n.Title, n.Tags, n.Project()); len(sim) > 0 {
				for _, s := range sim {
					if containsStr(s.Tags, "distinct") {
						continue
					}
					out = append(out, Pair{A: n, B: s})
					break
				}
			}
		}
		seen = append(seen, n)
	}
	return out
}
