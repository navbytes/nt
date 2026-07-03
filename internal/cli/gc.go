package cli

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
)

// gcCandidate is one note the collector proposes to trash, with its reason.
type gcCandidate struct {
	n      *note.Note
	reason string
}

// gcCandidates finds the two classes of dead weight a maturing store
// accumulates (both invisible to index/search/recall, both permanent today):
//
//   - superseded stubs: notes replaced via supersede/superseded_by. The pointer
//     preserves the decision trail for a grace period, after which the stub is
//     just a file in every git diff.
//   - stranded task-detail notes: machine notes under notes/__tasks__/ whose
//     task no longer exists anywhere (tasks.txt or done.txt) — removed tasks
//     leave their detail behind.
//
// cutoff is the YYYY-MM-DD before which a note must have last changed to
// qualify — recent items stay, in case of an undo or an in-flight rewrite.
func gcCandidates(e *mutate.Engine, cutoff string) []gcCandidate {
	notes, _ := note.List(e.S)
	// One haystack for "is this detail note still referenced": both task files,
	// raw. Substring matching on [[title]] is deliberately conservative — a false
	// "still referenced" keeps a file; a false "stranded" would trash a live one.
	var refs strings.Builder
	if data, err := store.ReadFile(e.S.TasksFile()); err == nil {
		refs.Write(data)
	}
	if data, err := os.ReadFile(e.S.DoneFile()); err == nil {
		refs.Write(data)
	}
	haystack := refs.String()

	var out []gcCandidate
	for _, n := range notes {
		if n.ChangedDate() > cutoff {
			continue
		}
		switch {
		case n.SupersededBy != "":
			out = append(out, gcCandidate{n, "superseded (replaced by " + shortID(n.SupersededBy) + ")"})
		case n.Reserved():
			if !strings.Contains(haystack, "[["+n.Title+"]]") {
				out = append(out, gcCandidate{n, "stranded task detail (its task no longer exists)"})
			}
		}
	}
	return out
}

// pastRelRe matches a past-relative day count: "30d" or "-30d" = 30 days ago
// (the same form dateparse.PastDate accepts for --updated-since).
var pastRelRe = regexp.MustCompile(`^-?(\d+)d$`)

// cmdGc reclaims dead weight: superseded stubs and stranded task-detail notes
// move to .trash/ (recoverable — same mechanism as `nt rm` on a note). Dry-run
// by default; --yes applies. Retention: --older-than 30d.
func cmdGc(args []string) int {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	olderThan := fs.String("older-than", "30d", "only collect notes unchanged for this long (Nd)")
	yes := fs.Bool("yes", false, "apply (default is a dry-run plan)")
	fs.BoolVar(yes, "y", false, "apply (default is a dry-run plan)")
	asJSON := fs.Bool("json", false, "print the plan/result as JSON")
	flags, _ := splitArgs(args, map[string]bool{"yes": true, "y": true, "json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	m := pastRelRe.FindStringSubmatch(strings.TrimSpace(*olderThan))
	if m == nil {
		return usageErr(fmt.Errorf("gc: --older-than wants a day count like 30d, got %q", *olderThan))
	}
	// pastRelRe's capture group is \d+, so this can't fail.
	days, _ := strconv.Atoi(m[1])
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	e, ok := engine()
	if !ok {
		return 1
	}
	cands := gcCandidates(e, cutoff)
	if len(cands) == 0 {
		fmt.Printf("nothing to gc — no superseded or stranded notes unchanged since %s\n", cutoff)
		return 0
	}

	if *asJSON {
		rows := make([]map[string]string, 0, len(cands))
		for _, c := range cands {
			rows = append(rows, map[string]string{"id": c.n.ID, "rel": c.n.Rel, "reason": c.reason, "updated": c.n.ChangedDate()})
		}
		if !*yes {
			return printJSON(map[string]any{"plan": rows, "applied": false, "hint": "rerun with --yes to move these to .trash/"})
		}
		for _, c := range cands {
			if err := e.TrashNote(c.n); err != nil {
				return fail(fmt.Errorf("gc: %s: %w", c.n.Rel, err))
			}
		}
		return printJSON(map[string]any{"collected": rows, "applied": true})
	}

	verb := "would collect"
	if *yes {
		verb = "collecting"
	}
	fmt.Printf("gc: %s %d note(s) unchanged since %s → .trash/ (recoverable)\n", verb, len(cands), cutoff)
	for _, c := range cands {
		fmt.Printf("  %s %s — %s (last change %s)\n", shortID(c.n.ID), c.n.Rel, c.reason, c.n.ChangedDate())
	}
	if !*yes {
		fmt.Println("dry-run — rerun with --yes to apply")
		return 0
	}
	for _, c := range cands {
		if err := e.TrashNote(c.n); err != nil {
			return fail(fmt.Errorf("gc: %s: %w", c.n.Rel, err))
		}
	}
	fmt.Printf("collected %d note(s) → .trash/\n", len(cands))
	return 0
}
