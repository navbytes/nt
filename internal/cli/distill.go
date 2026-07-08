package cli

import (
	"flag"
	"fmt"

	"github.com/navbytes/nt/internal/note"
)

// cmdDistill is the batch counterpart of the write-time near-duplicate guard
// (`nt note` refuses a near-dup; `nt_note` returns one as `similar`): it
// surfaces EVERY near-duplicate-title pair in the store at once — the same
// pairing `nt doctor` lints, but uncapped and with enough fields (id, rel,
// description, tags, updated) to review and merge without a separate fetch
// per pair. It never merges anything itself — human-gated consolidation
// means nt distill proposes, `nt_note_edit`/`nt edit` (fold the content in)
// plus `nt_archive superseded_by`/`nt supersede` (retire the old one, or `nt
// tag <id> +distinct` to keep both on purpose) execute, same as the
// write-time flow already does one note at a time.
//
//	nt distill               # every near-duplicate pair, human-readable
//	nt distill --json        # same, structured for an agent
//	nt distill --limit 10    # cap the list (0 = all, the default)
func cmdDistill(args []string) int {
	fs := flag.NewFlagSet("distill", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print pairs as JSON")
	limit := fs.Int("limit", 0, "cap the number of pairs returned (0 = all)")
	flags, _ := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	active := note.Active(mustNotes(e))
	pairs := note.NearDupPairs(active)
	truncated := false
	if *limit > 0 && len(pairs) > *limit {
		pairs = pairs[:*limit]
		truncated = true
	}

	if *asJSON {
		out := make([]map[string]any, 0, len(pairs))
		for _, p := range pairs {
			out = append(out, map[string]any{"a": distillStub(p.A), "b": distillStub(p.B)})
		}
		payload := map[string]any{"pairs": out}
		if truncated {
			payload["truncated"] = true
		}
		return printJSON(payload)
	}

	if len(pairs) == 0 {
		fmt.Println("no near-duplicate notes found")
		return 0
	}
	fmt.Printf("%d near-duplicate pair(s):\n\n", len(pairs))
	for i, p := range pairs {
		fmt.Printf("%d. %s\n", i+1, distillLine(p.A))
		fmt.Printf("   %s\n", distillLine(p.B))
		fmt.Printf("   → review: nt show %s / nt show %s\n", shortID(p.A.ID), shortID(p.B.ID))
		fmt.Printf("   → merge: nt edit %s --append \"…\" && nt supersede %s --by %s\n", shortID(p.A.ID), shortID(p.B.ID), shortID(p.A.ID))
		fmt.Printf("   → keep both (deliberate fork): nt tag %s +distinct\n\n", shortID(p.A.ID))
	}
	if truncated {
		fmt.Printf("(more exist — raise or drop --limit to see them all)\n")
	}
	return 0
}

// distillLine renders one note of a pair for the human-readable listing.
func distillLine(n *note.Note) string {
	line := fmt.Sprintf("%s  %s  %s", shortID(n.ID), n.Rel, n.Title)
	if d := n.Description(160); d != "" && d != n.Title {
		line += " — " + d
	}
	if u := n.ChangedDate(); u != "" {
		line += "  (" + u + ")"
	}
	return line
}

// distillStub is the JSON shape for one note in a pair — a stub, not the
// full body: an agent that decides to merge fetches bodies on demand
// (nt_get/nt show), the same progressive-disclosure shape nt_index/nt_search
// already use.
func distillStub(n *note.Note) map[string]any {
	out := map[string]any{
		"id": n.ID, "title": n.Title, "rel": n.Rel, "tags": n.Tags,
	}
	if d := n.Description(160); d != "" && d != n.Title {
		out["description"] = d
	}
	if u := n.ChangedDate(); u != "" {
		out["updated"] = u
	}
	return out
}
