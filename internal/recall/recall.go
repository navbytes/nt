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
var synGroups = [][]string{
	{"concurrency", "concurrent", "goroutine", "parallel", "async", "await", "race", "deadlock", "mutex", "lock", "thread", "singleflight"},
	{"deploy", "deployment", "release", "ship", "production", "prod", "rollout", "publish"},
	{"migration", "migrate", "schema", "database", "ddl", "alter", "column", "index"},
	{"auth", "authentication", "authorization", "login", "token", "jwt", "session", "oauth", "credential"},
	{"cors", "options", "preflight", "origin", "browser"},
	{"test", "testing", "flaky", "isolation", "fixture", "mock", "stub", "assertion"},
	{"cache", "caching", "invalidation", "ttl", "stale", "memoize"},
	{"timeout", "retry", "backoff", "deadline", "latency", "slow", "hang"},
	{"config", "configuration", "env", "environment", "flag", "setting", "variable"},
	{"error", "panic", "crash", "exception", "failure", "bug", "nil", "null"},
	{"memory", "leak", "allocation", "gc", "oom", "buffer"},
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

// stem is a naive suffix stripper: enough to fold deploys→deploy, deadlocked→
// deadlock, testing→test without a real stemmer's cost or surprises.
func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(suf)+2 && strings.HasSuffix(w, suf) {
			return w[:len(w)-len(suf)]
		}
	}
	return w
}

func notWord(r rune) bool {
	return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
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
	type scored struct {
		Result
		exact int // # of exact-word matches — the tie-breaker
	}
	var out []scored
	for _, n := range notes {
		if n.Reserved() {
			continue
		}
		strong := newBag(n.Title + " " + strings.Join(n.Tags, " ") + " " + n.Description(240))
		weak := newBag(n.Body)
		score, exact := 0, 0
		for w := range q.words {
			c := conceptID(w)
			switch {
			case strong.words[w]: // exact word in a high-signal field — strongest
				score += 4
				exact++
			case strong.concepts[c]: // synonym of it in a high-signal field
				score += 2
			case weak.words[w]: // exact word in the body
				score += 2
				exact++
			case weak.concepts[c]: // synonym in the body — weakest
				score++
			}
		}
		if score == 0 {
			continue
		}
		isLesson := false
		for _, t := range n.Tags {
			if t == LessonTag {
				isLesson = true
				break
			}
		}
		if isLesson {
			score += 5 // surface recorded mistakes above adjacent reference notes
		}
		out = append(out, scored{Result{Note: n, Score: score, Lesson: isLesson}, exact})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
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
