package note

// Pair is one near-duplicate-by-title match — shared by `nt doctor`'s
// near-dup lint (CLI), `nt distill`/`nt_distill` (uncapped, full fields), and
// anywhere else two notes need to be flagged as probably-the-same-thing.
type Pair struct{ A, B *Note }

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
			if sim := FindSimilar(seen, n.Title, n.Tags); len(sim) > 0 {
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
