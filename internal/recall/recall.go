// Package recall ranks notes by relevance to a free-text task context so a
// session can surface the lessons/gotchas a past session recorded BEFORE it
// repeats the mistake — the "learn from previous sessions" loop.
//
// Why this exists: nt_search is substring-AND (every term must appear verbatim),
// which misses paraphrases — an agent about to add "parallel request handling"
// won't type the exact words of a note titled "goroutine deadlock". recall trades
// precision for recall in the one place that needs it: it tokenizes the context,
// drops stopwords, applies light stemming, and expands each token across a small
// map of dev-concept synonyms, then scores notes by concept overlap. Notes tagged
// `lesson` are boosted so durable gotchas rise above ordinary reference notes.
//
// It is deliberately dependency-free (no embeddings/index) — a good-enough recall
// lift that stays instant on a plain-file store. When that ceases to be enough
// (very large corpora, subtle paraphrase), a vector index is the natural next step.
package recall

import (
	"math"
	"sort"
	"strings"

	"github.com/navbytes/nt/internal/note"
)

// LessonTag marks a note as a durable lesson/gotcha — the class recall surfaces
// first. Capture with `nt note --lesson` (or tag an existing note `lesson`).
const LessonTag = "lesson"

// Result is one ranked note. Lesson notes sort first at equal relevance.
type Result struct {
	Note   *note.Note
	Score  int
	Lesson bool
}

// synGroups cluster words that mean the same thing to a coding agent. Matching any
// member expands to the whole group, so a paraphrased query still reaches a note
// worded differently. Small and dev-focused on purpose — precision matters too.
//
// Groups are kept NARROW and non-overlapping: ambiguous cross-domain tokens
// (column, index, origin, lock, database) are deliberately NOT grouped, because
// one overloaded word ("column" → migration) would otherwise drag a whole wrong
// domain into an unrelated query (a CSS-column question surfacing DB migrations).
// nil/null and panic/crash are separate groups so distinct failure modes don't
// collapse into one bucket.
var synGroups = [][]string{
	{"concurrency", "concurrent", "goroutine", "parallel", "async", "await", "race", "deadlock", "mutex", "semaphore", "thread", "singleflight"},
	{"deploy", "deployment", "release", "ship", "production", "prod", "rollout", "canary"},
	{"migration", "migrate", "schema", "ddl", "alter"},
	{"auth", "authentication", "authorization", "login", "jwt", "oauth", "credential"},
	{"cors", "preflight"},
	{"test", "testing", "flaky", "fixture", "mock", "stub"},
	{"cache", "caching", "invalidation", "memoize", "redis"},
	{"timeout", "retry", "backoff", "deadline", "latency"},
	{"config", "configuration", "setting", "dotenv"},
	{"panic", "crash", "segfault", "stacktrace"},
	{"nil", "null", "nullpointer", "npe", "nullptr"},
	{"leak", "oom", "allocation", "gc"},
	// Domains a coding agent hits that the map was previously blind to:
	{"css", "flexbox", "flex", "grid", "layout", "overflow", "responsive", "viewport", "zindex"},
	{"billing", "payment", "invoice", "charge", "webhook", "idempotency", "refund", "stripe", "subscription"},
	{"i18n", "l10n", "locale", "translation", "rtl", "localization"},
}

// conceptOf maps a stemmed word to its group id ("g0", "g1", …) if it belongs to a
// synonym group, else to itself — the canonical token used for overlap scoring.
var conceptOf = func() map[string]string {
	m := map[string]string{}
	for i, g := range synGroups {
		id := "g" + string(rune('0'+i))
		for _, w := range g {
			m[stem(w)] = id
		}
	}
	return m
}()

// stop is a tiny stopword set — words too common to carry retrieval signal.
var stop = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true,
	"in": true, "on": true, "for": true, "with": true, "is": true, "are": true, "be": true,
	"it": true, "this": true, "that": true, "when": true, "how": true, "do": true, "i": true,
	"my": true, "we": true, "add": true, "use": true, "using": true, "new": true, "some": true,
	"about": true, "into": true, "from": true, "at": true, "by": true, "as": true, "not": true,
}

// stem is a light suffix stripper — enough to fold plural/verb forms to a common
// root so a query word matches a note's differently-inflected word. It is applied
// to BOTH query and note text, so it only needs to be self-consistent (map both
// "races" and "race" to the same token), not linguistically perfect.
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		w = w[:len(w)-3] + "y" // retries→retry, libraries→library
	case len(w) > 4 && strings.HasSuffix(w, "es"):
		w = w[:len(w)-2] // boxes→box, matches→match, caches→cach (canonicalized below)
	case len(w) > 4 && strings.HasSuffix(w, "ing"):
		w = w[:len(w)-3]
	case len(w) > 4 && strings.HasSuffix(w, "ed"):
		w = w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		w = w[:len(w)-1]
	}
	// Canonicalize a trailing 'e' so cache/caches and race/races fold to the same
	// token (English -es is inconsistent; folding both sides makes stem stable).
	if len(w) > 3 && strings.HasSuffix(w, "e") {
		w = w[:len(w)-1]
	}
	return w
}

func notWord(r rune) bool {
	return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
}

// bag is a note's (or query's) content reduced for matching: the set of stemmed
// words it contains, and the set of canonical concepts (synonym-group ids) those
// words map to. Keeping both lets scoring reward an EXACT word match above a mere
// same-concept (synonym) match — so a query for "goroutine" ranks the note that
// says "goroutine" over one that only says "parallel".
type bag struct {
	words    map[string]bool
	concepts map[string]bool
}

func newBag(s string) bag {
	b := bag{words: map[string]bool{}, concepts: map[string]bool{}}
	for _, raw := range strings.FieldsFunc(strings.ToLower(s), notWord) {
		w := stem(raw)
		if len(w) < 2 || stop[w] {
			continue
		}
		b.words[w] = true
		if c, ok := conceptOf[w]; ok {
			b.concepts[c] = true
		} else {
			b.concepts[w] = true
		}
	}
	return b
}

// conceptID returns the canonical concept for a stemmed query word.
func conceptID(w string) string {
	if c, ok := conceptOf[w]; ok {
		return c
	}
	return w
}

// Rank scores active notes against the task context and returns the most relevant,
// lesson notes boosted. A note scores 3 per concept matched in its title/tags/
// description (high-signal fields) or 1 in its body; lesson notes get a flat boost
// so a relevant gotcha outranks a merely-adjacent reference note. Notes with no
// overlap are dropped. limit<=0 means no cap.
func Rank(notes []*note.Note, context string, limit int) []Result {
	q := newBag(context)
	if len(q.words) == 0 {
		return nil
	}
	// Pass 1: build each note's bags and tally document frequency per concept, so a
	// common word ("database", "test") counts less than a rare, discriminating one.
	type cand struct {
		n            *note.Note
		strong, weak bag
		lesson       bool
	}
	var cands []cand
	df := map[string]int{}
	for _, n := range notes {
		if n.Reserved() {
			continue
		}
		c := cand{
			n:      n,
			strong: newBag(n.Title + " " + strings.Join(n.Tags, " ") + " " + n.Description(240)),
			weak:   newBag(n.Body),
		}
		for _, t := range n.Tags {
			if t == LessonTag {
				c.lesson = true
				break
			}
		}
		seen := map[string]bool{}
		for k := range c.strong.concepts {
			seen[k] = true
		}
		for k := range c.weak.concepts {
			seen[k] = true
		}
		for k := range seen {
			df[k]++
		}
		cands = append(cands, c)
	}
	n := len(cands)
	idf := func(concept string) float64 {
		d := df[concept]
		if d < 1 {
			d = 1
		}
		return math.Log(1 + float64(n)/float64(d))
	}
	// Pass 2: score. Per query concept: exact word in a high-signal field is
	// strongest, then a synonym there, then the body — each weighted by the
	// concept's IDF. The lesson boost is MULTIPLICATIVE (not a flat add), so it
	// tilts ties toward recorded mistakes without letting a one-concept lesson
	// outrank a genuinely more-relevant note.
	type scored struct {
		Result
		f     float64
		exact int
	}
	var out []scored
	for _, cd := range cands {
		var f float64
		exact := 0
		for w := range q.words {
			c := conceptID(w)
			var base float64
			switch {
			case cd.strong.words[w]:
				base, exact = 4, exact+1
			case cd.strong.concepts[c]:
				base = 2
			case cd.weak.words[w]:
				base, exact = 2, exact+1
			case cd.weak.concepts[c]:
				base = 1
			}
			f += base * idf(c)
		}
		if f == 0 {
			continue
		}
		if cd.lesson {
			f *= 1.6 // surface recorded mistakes, without swamping relevance
		}
		out = append(out, scored{Result{Note: cd.n, Score: int(f*100 + 0.5), Lesson: cd.lesson}, f, exact})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].f != out[j].f {
			return out[i].f > out[j].f
		}
		if out[i].exact != out[j].exact {
			return out[i].exact > out[j].exact // more exact-word hits wins the tie
		}
		if out[i].Lesson != out[j].Lesson {
			return out[i].Lesson
		}
		return out[i].Note.Updated > out[j].Note.Updated
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	res := make([]Result, len(out))
	for i := range out {
		res[i] = out[i].Result
	}
	return res
}
