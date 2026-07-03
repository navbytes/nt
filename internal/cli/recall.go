package cli

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/recall"
	"github.com/navbytes/nt/internal/workstream"
)

// cmdRecall surfaces the notes — lessons/gotchas first — most relevant to a
// free-text task context, so a session reads what a past session learned BEFORE
// repeating the mistake. Unlike `nt search` (substring-AND, exact terms), recall
// tokenizes the context, stems it, and expands dev-concept synonyms, so a
// paraphrase still finds the lesson.
//
//	nt recall adding a goroutine per request      # → the deadlock lesson, even so worded
//	nt recall deploying to prod --lessons-only     # only lesson-tagged notes
func cmdRecall(args []string) int {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	limit := fs.Int("limit", 8, "max results (0 = all)")
	lessonsOnly := fs.Bool("lessons-only", false, "only notes tagged 'lesson'")
	project := fs.String("project", "", "prefer this project's notes in ranking (default: NT_WORKSTREAM; 'none' disables)")
	asJSON := fs.Bool("json", false, "print results as JSON stubs")
	flags, positional := splitArgs(args, map[string]bool{"json": true, "lessons-only": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	context := strings.TrimSpace(strings.Join(positional, " "))
	if context == "" && !*lessonsOnly {
		return usageErr(fmt.Errorf("recall: describe what you're about to work on, e.g. `nt recall adding a cache layer` (or `nt recall --lessons-only` to list every recorded lesson)"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	notes := note.Active(mustNotes(e))
	if *lessonsOnly {
		kept := notes[:0]
		for _, n := range notes {
			if contains(n.Tags, recall.LessonTag) {
				kept = append(kept, n)
			}
		}
		notes = kept
	}
	var results []recall.Result
	if context == "" {
		// Bare `nt recall --lessons-only`: enumerate the whole lesson book, newest
		// first — the discoverable "what mistakes are on record?" read.
		for _, n := range notes {
			results = append(results, recall.Result{Note: n, Lesson: true})
		}
		sort.SliceStable(results, func(i, j int) bool {
			ui, uj := results[i].Note.Updated, results[j].Note.Updated
			if ui == "" {
				ui = results[i].Note.Created
			}
			if uj == "" {
				uj = results[j].Note.Created
			}
			return ui > uj
		})
		if *limit > 0 && len(results) > *limit {
			results = results[:*limit]
		}
	} else {
		// Same-project notes get a soft ranking preference — sourced from an
		// explicit --project, else the workstream identity. Cross-project results
		// stay visible below (knowledge cross-pollinates); 'none' disables.
		proj := strings.TrimSpace(*project)
		switch proj {
		case "":
			proj = workstream.Env()
		case "none", "-":
			proj = ""
		}
		results = recall.RankProject(notes, context, *limit, proj)
	}

	if *asJSON {
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			row := map[string]any{
				"id": r.Note.ID, "title": r.Note.Title, "description": r.Note.Description(160),
				"tags": r.Note.Tags, "folder": noteFolder(r.Note), "lesson": r.Lesson, "score": r.Score,
			}
			if r.ProjectMatch {
				row["projectMatch"] = true
			}
			out = append(out, row)
		}
		return printJSON(out)
	}
	if len(results) == 0 {
		fmt.Println("no relevant notes — nothing recorded for this context yet")
		return 0
	}
	for _, r := range results {
		mark := " "
		if r.Lesson {
			mark = "⚑" // a recorded lesson — read before proceeding
		}
		line := fmt.Sprintf("%s %s  %s", mark, shortID(r.Note.ID), r.Note.Title)
		if d := r.Note.Description(160); d != "" {
			line += " — " + d
		}
		if len(r.Note.Tags) > 0 {
			line += "  @" + strings.Join(r.Note.Tags, " @")
		}
		fmt.Println(line)
	}
	return 0
}
