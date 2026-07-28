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
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/note"
)

// LessonTag marks a note as a durable lesson/gotcha — the class recall surfaces
// first. Capture with `nt note --lesson` (or tag an existing note `lesson`).
const LessonTag = "lesson"

// expiredPenalty down-ranks (never hides) a note whose valid_until has
// passed: it's still findable — the fact might still matter, or the caller
// might specifically want the history — but a fresher, still-valid note on
// the same topic should usually win the tie. Multiplicative, like the lesson
// boost, so it scales with relevance rather than flatly subtracting.
const expiredPenalty = 0.4

// Result is one ranked note. Lesson notes sort first at equal relevance.
type Result struct {
	Note         *note.Note
	Score        int
	Lesson       bool
	ProjectMatch bool // note belongs to the caller's project (soft ranking boost applied)
	Expired      bool // note.Note.Expired() as of ranking time — valid_until has passed
	// Confidence is the PRE-BOOST score over the best score this query could
	// possibly award (see fMax below) — comparable across queries and store
	// sizes, unlike Score, which is IDF-scaled and therefore query-dependent.
	// Boosts (lesson/project/expired) are deliberately excluded: they express
	// preference, not evidence, and folding them in would let a thinly-matched
	// lesson print as confident — the exact promotion pathology the precision
	// floor already fights. Matched/QueryTerms are the raw coverage fraction
	// (m/n query words that hit at all), shown alongside the tier because a
	// tier alone hides *how much* of the query a note actually covers.
	Confidence float64
	Matched    int
	QueryTerms int
}

// Tier thresholds, calibrated against the paraphrase corpus (see
// TestParaphraseCorpusConfidence) and the real-store tailwind-dark-mode
// acceptance case: every corpus #1 hit clears "medium", and a nonsense query
// against a real store does not present as "strong". Boundaries are two named
// constants (not scattered magic numbers) precisely so recalibration is a
// one-line change with a test that catches drift.
const (
	// tierStrong sits a notch above the design doc's illustrative "exact match
	// on every term, but only in the body" worked example (0.50): a real-store
	// probe surfaced a note that verbatim QUOTES a test query as meta-commentary
	// (a field-test agent recording "recall X returns noise" using X as literal
	// example text) — full body-exact coverage on every term, landing at
	// precisely 0.50. Bag-of-words scoring cannot tell "topically about X" from
	// "contains the string X while talking about something else"; 0.55 costs
	// nothing against genuine hits (title/tag matches clear 0.6+ easily; see
	// TestParaphraseCorpusConfidence) and keeps that coincidence out of "strong".
	tierStrong = 0.55
	tierMedium = 0.15
)

// Tier buckets Confidence into a word an agent never has to interpret as a
// float: "strong" | "medium" | "weak". Kept as a method (not a package func)
// so CLI and MCP read it off the Result and can't drift into re-deriving it.
func (r Result) Tier() string {
	switch {
	case r.Confidence >= tierStrong:
		return "strong"
	case r.Confidence >= tierMedium:
		return "medium"
	default:
		return "weak"
	}
}

// synGroups cluster words that mean the same thing to a coding agent. Matching any
// member expands to the whole group, so a paraphrased query still reaches a note
// worded differently. Small and dev-focused on purpose — precision matters too.
//
// Groups are kept NARROW and non-overlapping: ambiguous cross-domain tokens
// (column, index, origin, lock, database, gc) are deliberately NOT grouped, because
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
	{"leak", "oom", "allocation", "heap"},
	// Domains a coding agent hits that the map was previously blind to:
	{"css", "flexbox", "flex", "grid", "layout", "overflow", "responsive", "viewport", "zindex"},
	{"billing", "payment", "invoice", "charge", "webhook", "idempotency", "refund", "stripe", "subscription"},
	{"i18n", "l10n", "locale", "translation", "rtl", "localization"},
	// Everyday software-engineering vocabulary the table was blind to: a field
	// test of 48 natural paraphrase pairs from a Go CLI codebase linked only 3.
	// Same rule as above — narrow, and no token that carries a second domain
	// (flag/option and module/package are deliberately absent: a feature flag
	// and a CLI flag are not the same thing).
	{"undo", "revert", "rollback", "undelete"},
	{"lint", "linter", "vet", "staticcheck", "gofmt", "formatter"},
	{"duplicate", "dedupe", "deduplicate", "duplication", "clobber", "overwrite"},
	{"regex", "regexp", "matcher"},
	{"serialize", "serialization", "marshal", "unmarshal", "encode", "decode"},
	{"dependency", "dependabot", "vendoring", "bump", "upgrade"},
	{"ci", "cicd", "pipeline"},
	{"frontmatter", "yaml", "toml"},
}

// buildConceptOf maps each group's stemmed words to a shared group id ("g0",
// "g1", …). fmt.Sprintf, not "g"+string(rune('0'+i)): the latter overflows
// past i=9 into punctuation (':' at i=10, ';' at i=11, …) and eventually
// collides with real word characters once the group count passes 74.
func buildConceptOf(groups [][]string) map[string]string {
	m := map[string]string{}
	for i, g := range groups {
		id := fmt.Sprintf("g%d", i)
		for _, w := range g {
			m[stem(w)] = id
		}
	}
	return m
}

// conceptOf maps a stemmed word to its group id if it belongs to a synonym
// group, else to itself — the canonical token used for overlap scoring.
// Starts as the built-in table; LoadUserSynonyms extends it.
var conceptOf = buildConceptOf(synGroups)

// LoadUserSynonyms reads $dir/synonyms.txt — one synonym group per line,
// words separated by commas/whitespace, '#' starts a comment, blank lines
// ignored — and merges it over the built-in table. A line sharing a word with
// a built-in group extends that group (e.g. add a house term to
// "concurrency" without knowing the rest of the list); an all-new line mints
// its own group. Cheap enough to call before every ranking — callers do, so
// an edited synonyms.txt takes effect on the next recall with no restart. A
// missing file is not an error — nothing to load; treat any other error as
// best-effort too (a malformed synonyms file shouldn't break recall).
func LoadUserSynonyms(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "synonyms.txt"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	groups := parseSynonymFile(string(data))
	if len(groups) == 0 {
		return nil
	}
	m := buildConceptOf(synGroups)
	nextID := len(synGroups)
	for _, g := range groups {
		targetID := ""
		for _, w := range g {
			if id, ok := m[stem(w)]; ok {
				targetID = id
				break
			}
		}
		if targetID == "" {
			targetID = fmt.Sprintf("g%d", nextID)
			nextID++
		}
		for _, w := range g {
			m[stem(w)] = targetID
		}
	}
	conceptOf = m
	return nil
}

// parseSynonymFile splits synonyms.txt into groups of >=2 words each — a
// solo word has nothing to synonym-match against, so single-word lines are
// dropped.
func parseSynonymFile(data string) [][]string {
	var groups [][]string
	for _, line := range strings.Split(data, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var words []string
		for _, f := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' || r == ' ' }) {
			if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
				words = append(words, f)
			}
		}
		if len(words) >= 2 {
			groups = append(groups, words)
		}
	}
	return groups
}

// stop is a stopword set — closed-class English words too common (in English
// generally, not just in this store) to carry retrieval signal. This matters
// more than it looks: IDF alone can't tell "uninformative in English" from
// "rare in this store" — on a real 218-note store, the modal "should" (df=5,
// IDF 3.69) outscored "swift", the headline token of an iOS-heavy store (IDF
// 3.12), and a function word in a title earned it the full strong-bag weight.
// Measured: the function-word share of the winning note's score was 0.02 on a
// hit vs 0.44 on a miss, and two misses were carried entirely by function
// words ("should"+"where"+"can", "where"+"keep") beating the actual target.
//
// Deliberately NOT stopped despite being common light verbs, because each is
// also a real technical term in this codebase's domain: "get"/"set" (HTTP
// GET, `go get`, getters/setters, Set data structures), "make" (Go's `make()`
// builtin, `make build`/Makefile targets).
var stop = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true,
	"in": true, "on": true, "for": true, "with": true, "is": true, "are": true, "be": true,
	"it": true, "this": true, "that": true, "when": true, "how": true, "do": true, "i": true,
	"my": true, "we": true, "add": true, "use": true, "using": true, "new": true, "some": true,
	"about": true, "into": true, "from": true, "at": true, "by": true, "as": true, "not": true,
	// Modals: never a topical term, always a hedge/permission/necessity marker.
	"should": true, "would": true, "could": true, "can": true, "must": true,
	"will": true, "might": true,
	// Wh-words: how/when were already covered; the rest of the question set.
	"where": true, "what": true, "which": true, "why": true,
	// Auxiliary "do"-support in questions ("does X work", "did it fail").
	"does": true, "did": true,
	// Light verbs with no technical sense of their own in this domain (unlike
	// get/set/make above): "keep the file", "need to fix".
	"keep": true, "need": true,
}

// stem is a light suffix stripper — enough to fold plural/verb forms to a common
// root so a query word matches a note's differently-inflected word. It is applied
// to BOTH query and note text, so it only needs to be self-consistent (map both
// "races" and "race" to the same token), not linguistically perfect.
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		w = w[:len(w)-3] + "y" // retries→retry, libraries→library
	// A plain 4-letter-noun + "s" plural (modes, names, files…) is not the same
	// pattern as a true sibilant -es plural (boxes, matches, caches): English
	// only inserts the extra vowel when the base ends in a sibilant sound.
	// Handled before the general "es" case below so "modes" keeps its silent e
	// like "mode" does, instead of folding to "mod" (see sibilantE).
	case len(w) == 5 && strings.HasSuffix(w, "es") && !sibilantE(w[:4]):
		w = w[:4] // modes→mode, names→name
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
	//
	// Guarded for a bare 4-letter word after a non-sibilant consonant (mode, not
	// cache/race): stripping those collides with an unrelated, shorter real word
	// that's meaningful in this store — "mode"→"mod" landed on the same stem as
	// the literal "mod" token split out of "go.mod", so a nonsense "dark mode"
	// query confidently matched unrelated go.mod notes. Longer words (5+) keep
	// the unconditional strip: it's what folds migrate/migrated,
	// duplicate/duplicated, etc. — their inflected forms are already stripped by
	// the "ed"/"es" cases above regardless of sibilance, so gating those too
	// would break that symmetry instead of fixing a real collision.
	if len(w) > 3 && strings.HasSuffix(w, "e") && (len(w) != 4 || sibilantE(w)) {
		w = w[:len(w)-1]
	}
	return w
}

// sibilantE reports whether w (which must end in "e") has a sibilant sound
// (s/x/z/soft c/soft g, or the "ch"/"sh" digraphs) right before that final e —
// the phonetic condition under which English actually needs the extra vowel to
// form a pronounceable plural (cache→caches, race→races). Words that fail this
// check (mode, name, file…) pluralize with a plain "+s" and should keep their e.
func sibilantE(w string) bool {
	if len(w) < 2 {
		return false
	}
	switch w[len(w)-2] {
	case 's', 'x', 'z', 'c', 'g':
		return true
	case 'h':
		return len(w) >= 3 && (w[len(w)-3] == 'c' || w[len(w)-3] == 's')
	default:
		return false
	}
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
	return RankProject(notes, context, limit, "")
}

// projectBoost tilts ties toward the caller's own project without burying
// cross-project knowledge: multi-project field use showed recall interleaving
// other projects' notes above the caller's equally-relevant ones. Kept well
// below the lesson boost (1.6) so a genuinely more-relevant foreign note still
// wins — this is a preference, not a filter.
const projectBoost = 1.25

// projectTokens reduces a project hint (an NT_WORKSTREAM like "feat-gamma-cache",
// a branch, or a plain project name) to its lowercase alphanumeric tokens, the
// form note tags fold to for matching.
func projectTokens(project string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(project), notWord) {
		if len(w) >= 2 && !stop[w] {
			out[w] = true
		}
	}
	return out
}

// matchesProject reports whether a note self-identifies as belonging to the
// project hint: any tag, folder path segment, or `project:` frontmatter token
// equals one of its tokens.
func matchesProject(n *note.Note, proj map[string]bool) bool {
	for _, t := range n.Tags {
		if proj[strings.ToLower(t)] {
			return true
		}
	}
	for _, seg := range strings.Split(strings.ToLower(n.Rel), "/") {
		if proj[seg] {
			return true
		}
	}
	if p := n.Project(); p != "" {
		for tok := range projectTokens(p) {
			if proj[tok] {
				return true
			}
		}
	}
	return false
}

// RankProject is Rank with a soft same-project preference: notes tagged (or
// foldered) as belonging to `project` — typically the caller's NT_WORKSTREAM —
// rank above equally-relevant notes from other projects. Empty project means
// no preference (identical to Rank).
func RankProject(notes []*note.Note, context string, limit int, project string) []Result {
	res, _ := rankProject(notes, context, limit, project, nil)
	return res
}

// candScore is a mid-pipeline candidate: the Result plus the bookkeeping
// needed to sort, floor, trim, and (when tracing) explain it. f is the FINAL
// (boosted) score used for ranking; raw is the pre-boost score Confidence is
// derived from. hits/excludeReason are only populated when trace != nil —
// building a TermHit slice per candidate on every call would be wasted work
// on the hot untraced path.
type candScore struct {
	Result
	f             float64
	exact         int
	matched       int
	raw           float64
	hits          []TermHit
	excludeReason string
	strongTerms   []string // only set for the ExplainNote target
}

// rankProject is RankProject's body. trace == nil is the normal hot path (one
// nil-check per candidate/term, no extra allocation); trace != nil records a
// term-by-term decomposition plus excluded candidates for ExplainProject/
// ExplainNote. Kept as a single function (not a second scorer) so the traced
// and untraced paths can never diverge — see TestExplainMatchesRank.
func rankProject(notes []*note.Note, context string, limit int, project string, trace *Trace) ([]Result, *Trace) {
	q := newBag(context)
	if len(q.words) == 0 {
		return nil, trace
	}
	proj := projectTokens(project)
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
	numNotes := len(cands)
	idf := func(concept string) float64 {
		d := df[concept]
		if d < 1 {
			d = 1
		}
		return math.Log(1 + float64(numNotes)/float64(d))
	}
	// Iterate query words in a FIXED order: float accumulation is not
	// associative, so map-order iteration makes tied notes differ in their last
	// bits run-to-run — which reshuffled results (and the --limit cutoff!) on
	// every invocation for homogeneous stores.
	qwords := make([]string, 0, len(q.words))
	for w := range q.words {
		qwords = append(qwords, w)
	}
	sort.Strings(qwords)
	// fMax is the best score ANY note could earn against this query — every
	// term matched exact-in-title (base 4) — so raw/fMax is comparable across
	// queries and store sizes by construction (numerator and denominator carry
	// the same IDF mass). idf(concept) doesn't depend on the candidate, so this
	// is computed once, not per note.
	fMax := 0.0
	for _, w := range qwords {
		fMax += 4 * idf(conceptID(w))
	}
	// Pass 2: score. Per query concept: exact word in a high-signal field is
	// strongest, then a synonym there, then the body — each weighted by the
	// concept's IDF. The lesson boost is MULTIPLICATIVE (not a flat add), so it
	// tilts ties toward recorded mistakes without letting a one-concept lesson
	// outrank a genuinely more-relevant note.
	var out []candScore
	for _, cd := range cands {
		var f float64
		exact, matched := 0, 0
		var hits []TermHit
		for _, w := range qwords {
			c := conceptID(w)
			var base float64
			where := ""
			switch {
			case cd.strong.words[w]:
				base, exact, where = 4, exact+1, "strong-exact"
			case cd.strong.concepts[c]:
				base, where = 2, "strong-syn"
			case cd.weak.words[w]:
				base, exact, where = 2, exact+1, "weak-exact"
			case cd.weak.concepts[c]:
				base, where = 1, "weak-syn"
			}
			if base > 0 {
				matched++
			}
			termIDF := idf(c)
			f += base * termIDF
			if trace != nil {
				hits = append(hits, TermHit{Term: w, Concept: c, Where: where, Base: base, IDF: termIDF})
			}
		}
		isTarget := trace != nil && trace.TargetID != "" && cd.n.ID == trace.TargetID
		if f == 0 {
			if isTarget {
				trace.Notes = append(trace.Notes, NoteTrace{
					ID: cd.n.ID, Title: cd.n.Title, Hits: hits,
					Excluded: "no-match", StrongTerms: strongTermsOf(cd.strong),
				})
			}
			continue
		}
		raw := f
		confidence := 0.0
		if fMax > 0 {
			confidence = raw / fMax
		}
		if cd.lesson {
			f *= 1.6 // surface recorded mistakes, without swamping relevance
		}
		isMine := len(proj) > 0 && matchesProject(cd.n, proj)
		if isMine {
			f *= projectBoost
		}
		expired := cd.n.Expired(time.Now())
		if expired {
			f *= expiredPenalty
		}
		var strongTerms []string
		if isTarget {
			strongTerms = strongTermsOf(cd.strong)
		}
		out = append(out, candScore{
			Result: Result{
				Note: cd.n, Score: int(f*100 + 0.5), Lesson: cd.lesson, ProjectMatch: isMine, Expired: expired,
				Confidence: confidence, Matched: matched, QueryTerms: len(qwords),
			},
			f: f, exact: exact, matched: matched, raw: raw, hits: hits, strongTerms: strongTerms,
		})
	}
	// Precision floor (field-study fix): a specific query (≥4 concepts) matching a
	// note on a SINGLE concept is topical noise, not a memory hit — the lesson
	// boost was promoting exactly those to the top of adjacent-topic queries. For
	// short queries a single shared concept is legitimately all the signal there is.
	//
	// The floor applies ONLY when something actually clears it. As an
	// unconditional filter it was a cliff in both directions: a heavily
	// paraphrased query that shares just one concept with its target returned
	// NOTHING, while the same intent in three words returned the right note —
	// so the documented recovery ("an empty result means nothing is recorded;
	// don't retry with looser words") was backwards, and the fix was to retry
	// SHORTER. When no candidate reaches two concepts, one shared concept is all
	// the signal the query carries, and the ranked list beats an empty answer;
	// the score the caller sees already reflects how weak the match is.
	var excluded []candScore
	floorActive := false
	if len(q.words) >= 4 {
		anyClears := false
		for _, s := range out {
			if s.matched >= 2 {
				anyClears = true
				break
			}
		}
		if anyClears {
			floorActive = true
			kept := out[:0]
			for _, s := range out {
				if s.matched >= 2 {
					kept = append(kept, s)
				} else if trace != nil {
					s.excludeReason = "precision-floor"
					excluded = append(excluded, s)
				}
			}
			out = kept
		}
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
		if out[i].Note.Updated != out[j].Note.Updated {
			return out[i].Note.Updated > out[j].Note.Updated
		}
		return out[i].Note.Rel < out[j].Note.Rel // total order: identical runs return identical results
	})
	// Trim the long tail: results far below the best hit read as "also relevant"
	// to an agent, which pads every recall to `limit` rows and buries the honest
	// "nothing more here". Anything under a quarter of the top score goes.
	if len(out) > 1 {
		floor := out[0].f * 0.25
		kept := out[:1]
		for _, s := range out[1:] {
			if s.f >= floor {
				kept = append(kept, s)
			} else if trace != nil {
				s.excludeReason = "tail-trim"
				excluded = append(excluded, s)
			}
		}
		out = kept
	}
	if limit > 0 && len(out) > limit {
		if trace != nil {
			for _, s := range out[limit:] {
				s.excludeReason = "limit"
				excluded = append(excluded, s)
			}
		}
		out = out[:limit]
	}
	if trace != nil {
		fillTrace(trace, qwords, numNotes, floorActive, out, excluded)
	}
	res := make([]Result, len(out))
	for i := range out {
		res[i] = out[i].Result
	}
	return res, trace
}
