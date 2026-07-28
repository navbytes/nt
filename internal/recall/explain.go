package recall

import (
	"fmt"
	"sort"

	"github.com/navbytes/nt/internal/note"
)

// explainExcludedCap bounds how many dropped candidates --explain prints.
// Without a cap a broad query against a big store would dump the entire
// precision-floor casualty list — flood, not diagnosis.
const explainExcludedCap = 5

// TermHit is one query term's outcome against one note. Where is empty when
// the term didn't match anything in the note at all.
type TermHit struct {
	Term    string  // stemmed query word
	Concept string  // synonym-group id ("g0"…), or the word itself if ungrouped
	Where   string  // "strong-exact" | "strong-syn" | "weak-exact" | "weak-syn" | ""
	Base    float64 // 4, 2, 2, 1, or 0 — matches Where
	IDF     float64
}

// NoteTrace is one note's full scoring decomposition: how each query term
// scored against it, what it summed to before/after boosts, and — for a
// dropped candidate — why it isn't in the printed results.
type NoteTrace struct {
	ID, Title                     string
	Hits                          []TermHit
	Raw, Final                    float64 // pre-boost / post-boost score
	Lesson, ProjectMatch, Expired bool    // which boosts fired
	Excluded                      string  // "" | "no-match" | "precision-floor" | "tail-trim" | "limit"
	// StrongTerms is the note's strong-bag (title/tags/description) vocabulary
	// after stemming — only populated for an ExplainNote target. It's the
	// actionable half of "why is my note missing": the words it WOULD need to
	// share with the query.
	StrongTerms []string
}

// Trace is the scoring trace for one recall call: ExplainProject fills Notes
// with the printed results plus up to explainExcludedCap dropped candidates;
// ExplainNote fills it with exactly one NoteTrace for the requested note,
// whether or not that note ever became a candidate.
type Trace struct {
	QueryTerms  []string // stemmed, sorted — the scorer's actual fixed iteration order
	NumNotes    int
	FloorActive bool
	TargetID    string // set by ExplainNote; empty for ExplainProject
	Notes       []NoteTrace
}

// ExplainProject is RankProject with a full scoring trace attached: which
// query terms hit which note at what strength, plus which candidates the
// precision floor, tail trim, or --limit dropped and why. Ranking itself is
// identical to RankProject/Rank — see TestExplainMatchesRank.
func ExplainProject(notes []*note.Note, context string, limit int, project string) ([]Result, *Trace) {
	return rankProject(notes, context, limit, project, &Trace{})
}

// ExplainNote traces ONE note against the query, whether or not it ever
// became a candidate — a note with zero term overlap never enters
// RankProject's output at all, so no list-level --explain can surface it.
// limit is unbounded: the target's own fate (kept, or dropped by the floor/
// trim/limit) must not depend on how many rows the caller asked to see.
func ExplainNote(notes []*note.Note, context, project, id string) (*Trace, error) {
	_, trace := rankProject(notes, context, 0, project, &Trace{TargetID: id})
	if len(trace.Notes) == 0 {
		return nil, fmt.Errorf("no note %q among the candidates for this query", id)
	}
	return trace, nil
}

// buildNoteTrace turns a scored candidate into its printable trace.
func buildNoteTrace(s candScore, reason string) NoteTrace {
	return NoteTrace{
		ID: s.Note.ID, Title: s.Note.Title, Hits: s.hits,
		Raw: s.raw, Final: s.f,
		Lesson: s.Lesson, ProjectMatch: s.ProjectMatch, Expired: s.Expired,
		Excluded: reason, StrongTerms: s.strongTerms,
	}
}

// strongTermsOf returns a note's strong-bag (title/tags/description)
// vocabulary, stemmed and sorted for stable output.
func strongTermsOf(b bag) []string {
	out := make([]string, 0, len(b.words))
	for w := range b.words {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// fillTrace assembles trace.Notes once ranking, the precision floor, tail
// trim, and --limit have all run. Guarded call from rankProject: only
// invoked when trace != nil.
func fillTrace(trace *Trace, qwords []string, numNotes int, floorActive bool, out, excluded []candScore) {
	trace.QueryTerms = qwords
	trace.NumNotes = numNotes
	trace.FloorActive = floorActive
	if trace.TargetID != "" {
		if len(trace.Notes) > 0 {
			return // already captured: the target scored 0 and never became a candidate
		}
		for _, s := range out {
			if s.Note.ID == trace.TargetID {
				trace.Notes = append(trace.Notes, buildNoteTrace(s, ""))
				return
			}
		}
		for _, s := range excluded {
			if s.Note.ID == trace.TargetID {
				trace.Notes = append(trace.Notes, buildNoteTrace(s, s.excludeReason))
				return
			}
		}
		return // note not among this query's candidates at all — ExplainNote errors
	}
	for _, s := range out {
		trace.Notes = append(trace.Notes, buildNoteTrace(s, ""))
	}
	sort.SliceStable(excluded, func(i, j int) bool { return excluded[i].f > excluded[j].f })
	if len(excluded) > explainExcludedCap {
		excluded = excluded[:explainExcludedCap]
	}
	for _, s := range excluded {
		trace.Notes = append(trace.Notes, buildNoteTrace(s, s.excludeReason))
	}
}
