package cli

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

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
//	nt recall adding a goroutine per request --explain          # why each result ranked
//	nt recall adding a goroutine per request --explain-note abc1  # why ONE note did/didn't
func cmdRecall(args []string) int {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	limit := fs.Int("limit", 8, "max results (0 = all)")
	lessonsOnly := fs.Bool("lessons-only", false, "only notes tagged 'lesson'")
	project := fs.String("project", "", "prefer this project's notes in ranking — matches tags, folders, and project: frontmatter (default: NT_WORKSTREAM; 'none' disables)")
	asJSON := fs.Bool("json", false, "print results as JSON stubs")
	explain := fs.Bool("explain", false, "show a term-by-term trace of why each result ranked, and what was excluded")
	explainNote := fs.String("explain-note", "", "trace one note (id/title/path) against the query, whether or not it scored")
	flags, positional := splitArgs(args, map[string]bool{"json": true, "lessons-only": true, "explain": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	context := strings.TrimSpace(strings.Join(positional, " "))
	if context == "" && !*lessonsOnly {
		return usageErr(fmt.Errorf("recall: describe what you're about to work on, e.g. `nt recall adding a cache layer` (or `nt recall --lessons-only` to list every recorded lesson)"))
	}
	if (*explain || *explainNote != "") && context == "" {
		return usageErr(fmt.Errorf("recall: --explain/--explain-note need a query to explain, e.g. `nt recall adding a cache layer --explain`"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	_ = recall.LoadUserSynonyms(e.S.Dir) // best-effort: a missing/bad file just means no extra synonyms
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

	if *explainNote != "" {
		target, err := resolveNote(notes, *explainNote)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		trace, err := recall.ExplainNote(notes, context, proj, target.ID)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if *asJSON {
			return printJSON(explainNoteJSON(trace))
		}
		printExplainTrace(trace, nil)
		return 0
	}

	var results []recall.Result
	var trace *recall.Trace
	if context == "" {
		// Bare `nt recall --lessons-only`: enumerate the whole lesson book, newest
		// first — the discoverable "what mistakes are on record?" read.
		for _, n := range notes {
			results = append(results, recall.Result{Note: n, Lesson: true, Expired: n.Expired(time.Now())})
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
	} else if *explain {
		results, trace = recall.ExplainProject(notes, context, *limit, proj)
	} else {
		results = recall.RankProject(notes, context, *limit, proj)
	}

	if *asJSON {
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			row := resultJSON(r)
			out = append(out, row)
		}
		if trace != nil {
			out = append(out, excludedJSON(trace)...)
		}
		return printJSON(out)
	}
	if *explain {
		printExplainTrace(trace, results)
		return 0
	}
	if len(results) == 0 {
		fmt.Println("no relevant notes — nothing recorded for this context yet")
		if context != "" {
			fmt.Printf("→ before concluding nothing is recorded: nt search %q --include-archived\n", context)
		}
		return 0
	}
	// A weak top hit means the store likely has nothing on this — printing a
	// ranked list without saying so reads as "recall found something relevant"
	// (the exact field-report failure: a confident-looking hit for noise). The
	// banner is a new first line, not a result row (starts with "~ " at column
	// 0, so a parser keyed on the existing ⚑/space marks skips it).
	if context != "" && results[0].QueryTerms > 0 && results[0].Tier() == "weak" {
		fmt.Println("~ all matches weak — likely nothing recorded for this; treat as no answer")
		fmt.Printf("→ escalate before trusting a weak hit: nt search %q --include-archived\n", context)
	}
	for _, r := range results {
		fmt.Println(resultLine(r))
	}
	return 0
}

// resultLine renders one plain-text result row: mark, id, title, description,
// tags, expired flag, then a confidence suffix — appended, like ⚠ expired, so
// column-0 marks and existing field order are untouched. The suffix is
// skipped for the bare `--lessons-only` enumeration (QueryTerms == 0: there
// was no query to score against).
func resultLine(r recall.Result) string {
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
	if r.Expired {
		line += "  ⚠ expired"
	}
	if r.QueryTerms > 0 {
		line += fmt.Sprintf("  [%s %d/%d]", r.Tier(), r.Matched, r.QueryTerms)
	}
	return line
}

// resultJSON is the JSON row for one Result. score stays for compat;
// confidence/tier/coverage are additive fields — SKILL.md tells agents to
// read tier, not score, since score is IDF-scaled and not comparable across
// queries.
func resultJSON(r recall.Result) map[string]any {
	row := map[string]any{
		"id": r.Note.ID, "title": r.Note.Title, "description": r.Note.Description(160),
		"tags": r.Note.Tags, "folder": noteFolder(r.Note), "lesson": r.Lesson, "score": r.Score,
	}
	if r.ProjectMatch {
		row["projectMatch"] = true
	}
	if r.Expired {
		row["expired"] = true
	}
	if r.QueryTerms > 0 {
		row["confidence"] = round2(r.Confidence)
		row["tier"] = r.Tier()
		row["coverage"] = fmt.Sprintf("%d/%d", r.Matched, r.QueryTerms)
	}
	return row
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// --- --explain rendering (plain text) -------------------------------------

// printExplainTrace renders a scoring trace: the query line, one block per
// note in trace.Notes (results first, then up to 5 excluded candidates in
// --explain, or the single target in --explain-note), and — in
// --explain-note mode — the target's strong-bag terms.
func printExplainTrace(trace *recall.Trace, results []recall.Result) {
	fmt.Printf("query: %s   [%d terms, %d notes%s]\n",
		strings.Join(trace.QueryTerms, " "), len(trace.QueryTerms), trace.NumNotes, floorSuffix(trace.FloorActive))
	tierOf := map[string]recall.Result{}
	for _, r := range results {
		tierOf[r.Note.ID] = r
	}
	explainNote := trace.TargetID != ""
	printedHeading := false
	for _, nt := range trace.Notes {
		if !explainNote && !printedHeading && nt.Excluded != "" {
			fmt.Println("— excluded (nearest 5) —")
			printedHeading = true
		}
		printNoteTraceBlock(nt, tierOf[nt.ID], explainNote)
	}
}

func floorSuffix(active bool) string {
	if active {
		return ", floor active"
	}
	return ""
}

// printNoteTraceBlock renders one note's block: a header line, its per-term
// hits, and a summary. Confidence is only known when the note is one of the
// call's own results (tierOf lookup hit) — an excluded-in-list-mode
// candidate never enters Result, so its block shows raw/final but not conf.
func printNoteTraceBlock(nt recall.NoteTrace, r recall.Result, explainNote bool) {
	mark := " "
	if nt.Lesson {
		mark = "⚑"
	}
	header := fmt.Sprintf("%s %s  %s", mark, shortID(nt.ID), nt.Title)
	conf := 0.0
	if r.Note != nil {
		conf = r.Confidence
	}
	switch {
	case nt.Excluded == "no-match":
		fmt.Printf("%s  score 0 — never a candidate\n", header)
	case !explainNote && nt.Excluded == "":
		fmt.Printf("%s  [%s %d/%d]\n", header, r.Tier(), r.Matched, r.QueryTerms)
	default:
		fmt.Printf("%s  raw %.2f (score %d)  conf %.2f%s\n", header, nt.Raw, int(nt.Final*100+0.5), conf, excludedTag(nt.Excluded))
	}
	printHits(nt.Hits)
	if !explainNote && nt.Excluded == "" {
		fmt.Printf("    raw %.2f → %.2f (score %d)  conf %.2f\n", nt.Raw, nt.Final, int(nt.Final*100+0.5), conf)
	}
	if nt.Excluded != "" && nt.Excluded != "no-match" {
		fmt.Printf("    %s\n", excludeDetail(nt))
	}
	if explainNote {
		fmt.Printf("    note strong-bag terms: %s\n", strings.Join(nt.StrongTerms, " "))
		if nt.Excluded == "no-match" {
			fmt.Println("    no shared term or synonym group; add tags or a synonyms.txt line")
		}
	}
}

func excludedTag(reason string) string {
	if reason == "" {
		return ""
	}
	return fmt.Sprintf("  [excluded: %s]", reason)
}

func excludeDetail(nt recall.NoteTrace) string {
	switch nt.Excluded {
	case "precision-floor":
		return "precision-floor  — a single shared concept on a long query is topical noise, not a hit"
	case "tail-trim":
		return fmt.Sprintf("tail-trim  raw %.2f is under 25%% of the top result", nt.Raw)
	case "limit":
		return "limit  ranked below --limit"
	default:
		return nt.Excluded
	}
}

func printHits(hits []recall.TermHit) {
	var noMatch []string
	for _, h := range hits {
		if h.Where == "" {
			noMatch = append(noMatch, h.Term)
			continue
		}
		term := h.Term
		if h.Concept != h.Term {
			term = fmt.Sprintf("%s (%s)", h.Term, h.Concept)
		}
		fmt.Printf("    %-16s%-14s%.0f × idf %.2f = %.2f\n", term, h.Where, h.Base, h.IDF, h.Base*h.IDF)
	}
	if len(noMatch) > 0 {
		// Not %-16s like the matched rows above: the joined list can run past
		// 16 chars (a query with several unmatched terms), which would butt
		// straight into "no match" with no separating space.
		fmt.Printf("    %s  no match\n", strings.Join(noMatch, ", "))
	}
}

// --- --explain JSON --------------------------------------------------------

func excludedJSON(trace *recall.Trace) []map[string]any {
	var out []map[string]any
	for _, nt := range trace.Notes {
		if nt.Excluded == "" {
			continue
		}
		out = append(out, map[string]any{
			"id": nt.ID, "title": nt.Title, "excluded": nt.Excluded,
			"raw": round2(nt.Raw), "score": int(nt.Final*100 + 0.5),
		})
	}
	return out
}

func explainNoteJSON(trace *recall.Trace) map[string]any {
	if len(trace.Notes) == 0 {
		return map[string]any{}
	}
	nt := trace.Notes[0]
	hits := make([]map[string]any, len(nt.Hits))
	for i, h := range nt.Hits {
		hits[i] = map[string]any{"term": h.Term, "concept": h.Concept, "where": h.Where, "base": h.Base, "idf": round2(h.IDF)}
	}
	row := map[string]any{
		"id": nt.ID, "title": nt.Title, "raw": round2(nt.Raw), "score": int(nt.Final*100 + 0.5),
		"hits": hits, "strongTerms": nt.StrongTerms,
	}
	if nt.Excluded != "" {
		row["excluded"] = nt.Excluded
	}
	return row
}
