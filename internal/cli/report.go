package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/view"
)

// Read/report commands: the non-mutating verbs that list, query, and render
// tasks/notes (list, ready, today, agenda, recall, search, links, log). Split out
// of commands.go, which keeps the mutating verbs + shared helpers (E6).

// freshHint returns a getting-started nudge to append to an empty-state message
// when the store has never been written to — so a brand-new user (whose first
// command might be `nt ready` or `nt today`, not the no-arg TUI) learns the next
// step. It stays silent for established users who simply have an empty list now.
func freshHint(e *mutate.Engine) string {
	if e != nil && e.S.IsFresh() {
		return "\n  add your first task:  nt add \"my first task\""
	}
	return ""
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	status := fs.String("status", "", "open|doing|blocked|done")
	tag := fs.String("tag", "", "filter by tag")
	project := fs.String("project", "", "filter by project")
	source := fs.String("source", "", "filter by source (e.g. claude)")
	sortBy := fs.String("sort", "", "urgency|due|created")
	all := fs.Bool("all", false, "include done tasks")
	showBlocked := fs.Bool("show-blocked", false, "include dependency-blocked tasks")
	tree := fs.Bool("tree", false, "show sub-tasks indented under their parent, with progress")
	asJSON := fs.Bool("json", false, "machine-readable output")

	flags, _ := splitArgs(args, map[string]bool{"all": true, "json": true, "show-blocked": true, "tree": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	return runListSource(view.Spec{
		Status:      *status,
		Tag:         *tag,
		Project:     *project,
		Sort:        *sortBy,
		All:         *all,
		ShowBlocked: *showBlocked,
		Tree:        *tree,
	}, *source, *asJSON)
}

// runList renders the task list selected by spec — the shared core behind both
// `nt list` and `nt view recall`, so a saved view filters/sorts exactly as the
// equivalent flags would. asJSON is a recall-time output choice, kept out of the
// saved Spec.
func runList(spec view.Spec, asJSON bool) int { return runListSource(spec, "", asJSON) }

// runListSource is runList with an extra source filter applied after view.Apply
// (view.Spec has no Source field, so the CLI filters it here for parity with
// ready/agenda/recall/log).
func runListSource(spec view.Spec, source string, asJSON bool) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}
	all3 := d.Tasks()
	idx := indexMap(all3)
	blocked := task.BlockedIDs(all3)
	rows := view.Apply(all3, spec, blocked)
	if source != "" {
		kept := rows[:0]
		for _, t := range rows {
			if t.Source() == source {
				kept = append(kept, t)
			}
		}
		rows = kept
	}

	if asJSON {
		return printJSON(tasksToJSON(rows, idx))
	}
	if len(rows) == 0 {
		fmt.Println("no tasks" + freshHint(e))
		return 0
	}
	if spec.Tree {
		printTaskTree(rows, idx, blocked)
		return 0
	}
	for _, t := range rows {
		fmt.Println(formatRow(t, idx[t], blocked[t.ID()]))
	}
	return 0
}

// printTaskTree renders tasks as a parent/child outline: roots first, each
// followed by its children indented, with a "(done/total)" progress marker on
// any task that has children. A task whose parent isn't in the visible set is
// treated as a root so nothing is hidden.
func printTaskTree(rows []*task.Task, idx map[*task.Task]int, blocked map[string]bool) {
	inSet := make(map[string]bool, len(rows))
	for _, t := range rows {
		inSet[t.ID()] = true
	}
	children := map[string][]*task.Task{}
	var roots []*task.Task
	for _, t := range rows {
		if p := t.Parent(); p != "" && inSet[p] {
			children[p] = append(children[p], t)
		} else {
			roots = append(roots, t)
		}
	}
	var walk func(t *task.Task, depth int)
	walk = func(t *task.Task, depth int) {
		indent := strings.Repeat("  ", depth)
		line := formatRow(t, idx[t], blocked[t.ID()])
		if kids := children[t.ID()]; len(kids) > 0 {
			done := 0
			for _, k := range kids {
				if k.Done {
					done++
				}
			}
			line += fmt.Sprintf("  (%d/%d)", done, len(kids))
		}
		fmt.Println(indent + line)
		for _, k := range children[t.ID()] {
			walk(k, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
}

// cmdReady lists open, unblocked tasks by urgency — the canonical "what should I
// pick up next" feed, and the recommended entry point for an AI session resuming
// work. It's the default actionable set (no done, no dependency-blocked tasks),
// urgency-sorted, optionally narrowed by source/tag/project.
func cmdReady(args []string) int {
	fs := flag.NewFlagSet("ready", flag.ContinueOnError)
	source := fs.String("source", "", "filter by source (e.g. claude)")
	tag := fs.String("tag", "", "filter by tag")
	project := fs.String("project", "", "filter by project")
	asJSON := fs.Bool("json", false, "machine-readable output")

	flags, _ := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}
	all := d.Tasks()
	idx := indexMap(all)
	blocked := task.BlockedIDs(all)

	today := mutate.Today()
	var rows []*task.Task
	for _, t := range all {
		// keep with all=false, showBlocked=false drops done + dependency-blocked.
		if !view.Keep(t, view.Spec{Tag: *tag, Project: *project}, blocked) {
			continue
		}
		if *source != "" && t.Source() != *source {
			continue
		}
		if isFutureStart(t, today) {
			continue // deferred: not actionable until its start (t:) date
		}
		rows = append(rows, t)
	}
	view.SortTasks(rows, "urgency")

	if *asJSON {
		return printJSON(tasksToJSON(rows, idx))
	}
	if len(rows) == 0 {
		fmt.Println("nothing ready — all clear, or everything open is blocked" + freshHint(e))
		return 0
	}
	for _, t := range rows {
		fmt.Println(formatRow(t, idx[t], false)) // ready ⇒ not blocked
	}
	return 0
}

// cmdToday is the day's actionable plan: overdue + due-today + just-became-
// actionable (start today). Shorthand for `agenda --days 0`.
func cmdToday(args []string) int { return runAgenda(args, 0) }

// cmdAgenda shows the date-windowed plan — overdue, today, and the next N days —
// grouped by bucket and urgency-sorted, so nt is a planner, not just a list. It
// excludes done, dependency-blocked, and not-yet-started (future t:) tasks.
func cmdAgenda(args []string) int {
	defDays := 7
	if c := loadConfig(); c.AgendaDays > 0 {
		defDays = c.AgendaDays // [defaults] agenda_days; an explicit --days still wins
	}
	return runAgenda(args, defDays)
}

func runAgenda(args []string, defDays int) int {
	fs := flag.NewFlagSet("agenda", flag.ContinueOnError)
	days := fs.Int("days", defDays, "horizon: include tasks due within N days")
	tag := fs.String("tag", "", "filter by tag")
	project := fs.String("project", "", "filter by project")
	source := fs.String("source", "", "filter by source")
	asJSON := fs.Bool("json", false, "machine-readable output")

	flags, _ := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}
	all := d.Tasks()
	idx := indexMap(all)
	blocked := task.BlockedIDs(all)
	today := mutate.Today()
	horizon := addDays(today, *days)

	// Select actionable tasks that land in the window: overdue, due within the
	// horizon, or newly actionable today (start == today).
	var rows []*task.Task
	for _, t := range all {
		if t.Done || (blocked[t.ID()] && !t.Done) || isFutureStart(t, today) {
			continue
		}
		if *tag != "" && !contains(t.Tags(), *tag) {
			continue
		}
		if *project != "" && !contains(t.Projects(), *project) {
			continue
		}
		if *source != "" && t.Source() != *source {
			continue
		}
		due := dateparse.DatePart(t.Due())      // ignore any time-of-day for windowing
		inWindow := due != "" && due <= horizon // overdue (< today) or within horizon
		startsToday := dateparse.DatePart(t.Start()) == today
		if inWindow || startsToday {
			rows = append(rows, t)
		}
	}
	view.SortTasks(rows, "urgency")

	if *asJSON {
		return printJSON(tasksToJSON(rows, idx))
	}
	if len(rows) == 0 {
		fmt.Println("nothing on the agenda — all clear within the horizon" + freshHint(e))
		return 0
	}
	// Group into Overdue / Today / Upcoming, preserving urgency order within each.
	buckets := []struct {
		label string
		keep  func(*task.Task) bool
	}{
		{"Overdue", func(t *task.Task) bool { d := dateparse.DatePart(t.Due()); return d != "" && d < today }},
		{"Today", func(t *task.Task) bool {
			d, st := dateparse.DatePart(t.Due()), dateparse.DatePart(t.Start())
			return d == today || (st == today && (d == "" || d >= today))
		}},
		{"Upcoming", func(t *task.Task) bool { return dateparse.DatePart(t.Due()) > today }},
	}
	for _, b := range buckets {
		var group []*task.Task
		for _, t := range rows {
			if b.keep(t) {
				group = append(group, t)
			}
		}
		if len(group) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", b.label)
		for _, t := range group {
			fmt.Println("  " + formatRow(t, idx[t], false))
		}
	}
	return 0
}

// isFutureStart reports whether a task is deferred — it has a start (t:) date
// that is still in the future, so it isn't actionable yet.
func isFutureStart(t *task.Task, today string) bool {
	s := dateparse.DatePart(t.Start())
	return s != "" && s > today
}

// addDays returns the ISO date n days after the given ISO date (n may be 0).
func addDays(date string, n int) string {
	tm, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return tm.AddDate(0, 0, n).Format("2006-01-02")
}

// searchTerms tokenizes a query into match terms: whitespace-separated words,
// each ANDed (order-independent), with "double quotes" grouping a multi-word
// exact phrase into one term. Returns lowercased terms.
func searchTerms(query string) []string {
	var terms []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			terms = append(terms, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			if inQuote {
				flush()
			}
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return terms
}

// matchesAll reports whether every term appears (case-insensitive substring) in
// the haystack — the AND semantics so `state management` finds a note containing
// both words in any order, not only the exact contiguous phrase.
func matchesAll(haystack string, terms []string) bool {
	h := strings.ToLower(haystack)
	for _, t := range terms {
		if !strings.Contains(h, t) {
			return false
		}
	}
	return true
}

// bestSnippet picks the body line covering the most query terms (for a one-row,
// context-bearing result), falling back to the title when the match is title-only.
func bestSnippet(n *note.Note, terms []string) string {
	best, bestScore := "", 0
	for _, line := range strings.Split(n.Body, "\n") {
		clean := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if clean == "" || clean == n.Title {
			continue // skip the auto-prepended "# Title" H1 — it just echoes the title
		}
		l := strings.ToLower(clean)
		score := 0
		for _, t := range terms {
			if strings.Contains(l, t) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = clean, score
		}
	}
	if best == "" {
		return n.Title
	}
	return best
}

type searchNoteJSON struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Rel     string   `json:"rel"`
	Path    string   `json:"path"`
	Tags    []string `json:"tags,omitempty"`
	Snippet string   `json:"snippet,omitempty"`
}

type searchJSON struct {
	Notes []searchNoteJSON `json:"notes"`
	Tasks []taskJSON       `json:"tasks"`
}

func cmdSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	typ := fs.String("type", "all", "note|task|all")
	asJSON := fs.Bool("json", false, "machine-readable output")
	var tags stringSlice
	fs.Var(&tags, "tag", "only items with this tag (repeatable, AND)")
	flags, positional := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	query := strings.Join(positional, " ")
	if query == "" && len(tags) == 0 {
		return usageErr(fmt.Errorf("search: need a query or --tag"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	terms := searchTerms(query)
	hasAll := func(have []string) bool {
		for _, w := range tags {
			if !contains(have, w) {
				return false
			}
		}
		return true
	}

	// Notes: one row per note, AND-matching every term across title+body, ranked
	// title-hits-first (the relevant note shouldn't sit below body-only matches).
	type noteHit struct {
		n          *note.Note
		titleMatch bool
		snippet    string
	}
	var noteHits []noteHit
	if *typ != "task" {
		notes := note.Active(mustNotes(e))
		for _, n := range notes {
			if len(tags) > 0 && !hasAll(n.Tags) {
				continue
			}
			if len(terms) > 0 && !matchesAll(n.Title+"\n"+n.Body, terms) {
				continue
			}
			h := noteHit{n: n, titleMatch: len(terms) == 0 || matchesAll(n.Title, terms)}
			if len(terms) > 0 {
				h.snippet = bestSnippet(n, terms)
			} else {
				h.snippet = n.Title
			}
			noteHits = append(noteHits, h)
		}
		sort.SliceStable(noteHits, func(i, j int) bool {
			if noteHits[i].titleMatch != noteHits[j].titleMatch {
				return noteHits[i].titleMatch // title matches first
			}
			return noteHits[i].n.Rel < noteHits[j].n.Rel
		})
	}

	// Tasks: AND-match the whole task line (text + tags + project).
	var taskHits []*task.Task
	var idx map[*task.Task]int
	var blocked map[string]bool
	if *typ != "note" {
		d, _ := e.Read()
		idx = indexMap(d.Tasks())
		blocked = task.BlockedIDs(d.Tasks())
		for _, t := range d.Tasks() {
			if len(tags) > 0 && !hasAll(t.Tags()) {
				continue
			}
			if len(terms) == 0 || matchesAll(t.Line(), terms) {
				taskHits = append(taskHits, t)
			}
		}
	}

	if *asJSON {
		out := searchJSON{Tasks: tasksToJSON(taskHits, idx)}
		for _, h := range noteHits {
			out.Notes = append(out.Notes, searchNoteJSON{
				ID: h.n.ID, Title: h.n.Title, Rel: h.n.Rel, Path: h.n.Path,
				Tags: h.n.Tags, Snippet: h.snippet,
			})
		}
		return printJSON(out)
	}

	found := 0
	for _, h := range noteHits {
		if h.snippet != "" && h.snippet != h.n.Title {
			fmt.Printf("note  %s  %s  %s — %s\n", shortID(h.n.ID), h.n.Rel, h.n.Title, h.snippet)
		} else {
			fmt.Printf("note  %s  %s  %s\n", shortID(h.n.ID), h.n.Rel, h.n.Title)
		}
		found++
	}
	for _, t := range taskHits {
		fmt.Println(formatRow(t, idx[t], blocked[t.ID()]))
		found++
	}
	if found == 0 {
		fmt.Println("no matches")
	}
	return 0
}

// mustNotes lists notes, tolerating an error as an empty set (read-only paths).
func mustNotes(e *mutate.Engine) []*note.Note {
	ns, _ := note.List(e.S)
	return ns
}

type taskRef struct {
	ID      string `json:"id"`
	ShortID string `json:"shortId"`
	Title   string `json:"title"`
}

type linkRef struct {
	Kind   string `json:"kind"` // task|note|unresolved|ambiguous
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Target string `json:"target,omitempty"`
}

type backRef struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type linksJSON struct {
	ID             string    `json:"id"`
	ShortID        string    `json:"shortId"`
	Title          string    `json:"title"`
	Kind           string    `json:"kind"`
	Forward        []linkRef `json:"forward"`
	LinkedFrom     []backRef `json:"linkedFrom"`
	DiscoveredFrom *taskRef  `json:"discoveredFrom,omitempty"`
	DiscoveredHere []taskRef `json:"discoveredHere,omitempty"`
	Blocks         []taskRef `json:"blocks,omitempty"`
	BlockedBy      []taskRef `json:"blockedBy,omitempty"`
	Parent         *taskRef  `json:"parent,omitempty"`
	Children       []taskRef `json:"children,omitempty"`
}

func taskRefOf(t *task.Task) taskRef {
	return taskRef{ID: t.ID(), ShortID: shortID(t.ID()), Title: t.Text}
}

func cmdLinks(args []string) int {
	fs := flag.NewFlagSet("links", flag.ContinueOnError)
	orphans := fs.Bool("orphans", false, "list notes with no inbound links (no handle needed)")
	asJSON := fs.Bool("json", false, "machine-readable output")
	flags, positional := splitArgs(args, map[string]bool{"orphans": true, "json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	if *orphans {
		found := 0
		for _, n := range notes {
			if n.Archived {
				continue // archived notes are retired, not orphans to reconnect
			}
			if len(links.Backlinks(e.S, n.ID, n.Rel)) == 0 {
				fmt.Printf("orphan  %s  %s\n", n.Rel, n.Title)
				found++
			}
		}
		if found == 0 {
			fmt.Println("no orphans")
		}
		return 0
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("links: need an id (or note:slug), or --orphans"))
	}
	handle := positional[0]
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}

	var id, title, noteRel string
	var forward []string
	var self *task.Task
	if strings.HasPrefix(handle, "note:") {
		want := strings.TrimPrefix(handle, "note:")
		it, ok := links.Resolve(want, nil, notes)
		if !ok {
			if it.Kind == "ambiguous" {
				return fail(fmt.Errorf("links: %q is ambiguous (%s) — qualify with a folder", want, it.Title))
			}
			return fail(fmt.Errorf("links: no note %q", want))
		}
		for _, n := range notes {
			if n.Path == it.Path {
				id, title, noteRel = n.ID, n.Title, n.Rel
				forward = extractLinks(n.Body)
				break
			}
		}
	} else if t, terr := resolveHandle(d, handle); terr == nil {
		id, title, self = t.ID(), t.Text, t
		forward = t.Links()
	} else if it, ok := links.Resolve(handle, nil, notes); ok && it.Kind == "note" {
		// Bare handle didn't match a task — accept a note handle (slug/title or the
		// short id `nt note` prints) so all verbs take the same handle.
		for _, n := range notes {
			if n.Path == it.Path {
				id, title, noteRel = n.ID, n.Title, n.Rel
				forward = extractLinks(n.Body)
				break
			}
		}
	} else {
		return fail(fmt.Errorf("links: %w", terr))
	}

	// Resolve forward links into structured refs (shared by JSON and text output).
	var fwd []linkRef
	for _, target := range forward {
		key, alias := links.NormalizeTarget(target)
		disp := key
		if alias != "" {
			disp = alias
		}
		switch it, ok := links.Resolve(target, d, notes); {
		case ok:
			fwd = append(fwd, linkRef{Kind: it.Kind, ID: it.ID, Title: it.Title})
		case it.Kind == "ambiguous":
			fwd = append(fwd, linkRef{Kind: "ambiguous", Title: it.Title, Target: disp})
		default:
			fwd = append(fwd, linkRef{Kind: "unresolved", Target: disp})
		}
	}

	back := links.Backlinks(e.S, id, noteRel)
	var backs []backRef
	for _, h := range back {
		// For a task, parent:/blocks: pointers also match by id; render those as the
		// semantic subtasks/blocked-by sections below, and keep "linked from" to real
		// [[wikilink]] references so the raw ULID tokens don't leak into the output.
		if self != nil && !strings.Contains(h.Text, "[[") {
			continue
		}
		rel, _ := filepath.Rel(e.S.Dir, h.Path)
		backs = append(backs, backRef{Path: rel, Line: h.Line, Text: strings.TrimSpace(h.Text)})
	}

	// Task structure & dependencies — discovered-from/here, blocks/blocked-by, and
	// parent/children — so an agent (or human) can reconstruct the full graph, not
	// just wikilinks. Previously these were invisible or leaked as raw ULID tokens.
	var discFrom, parentRef *taskRef
	var discHere, blocksR, blockedBy, children []taskRef
	if self != nil {
		if df := self.Discovered(); df != "" {
			if origin := d.FindByID(df); origin != nil {
				r := taskRefOf(origin)
				discFrom = &r
			}
		}
		if b := self.Blocks(); b != "" {
			if bt := d.FindByID(b); bt != nil {
				blocksR = append(blocksR, taskRefOf(bt))
			}
		}
		if p := self.Parent(); p != "" {
			if pt := d.FindByID(p); pt != nil {
				r := taskRefOf(pt)
				parentRef = &r
			}
		}
		for _, o := range d.Tasks() {
			if o.Discovered() == self.ID() {
				discHere = append(discHere, taskRefOf(o))
			}
			if o.Blocks() == self.ID() {
				blockedBy = append(blockedBy, taskRefOf(o))
			}
			if o.Parent() == self.ID() {
				children = append(children, taskRefOf(o))
			}
		}
	}

	kind := "note"
	if self != nil {
		kind = "task"
	}

	if *asJSON {
		out := linksJSON{
			ID: id, ShortID: shortID(id), Title: title, Kind: kind,
			Forward: fwd, LinkedFrom: backs,
			DiscoveredFrom: discFrom, DiscoveredHere: discHere,
			Blocks: blocksR, BlockedBy: blockedBy, Parent: parentRef, Children: children,
		}
		if out.Forward == nil {
			out.Forward = []linkRef{}
		}
		if out.LinkedFrom == nil {
			out.LinkedFrom = []backRef{}
		}
		return printJSON(out)
	}

	fmt.Printf("%s  %s\n", shortID(id), title)
	fmt.Println("forward:")
	if len(fwd) == 0 {
		fmt.Println("  (none)")
	}
	for _, r := range fwd {
		switch r.Kind {
		case "ambiguous":
			fmt.Printf("  → [[%s]] (ambiguous: %s)\n", r.Target, r.Title)
		case "unresolved":
			fmt.Printf("  → [[%s]] (unresolved)\n", r.Target)
		default:
			fmt.Printf("  → [%s] %s  %s\n", r.Kind, shortID(r.ID), r.Title)
		}
	}
	fmt.Println("linked from:")
	if len(backs) == 0 {
		fmt.Println("  (none)")
	}
	for _, h := range backs {
		fmt.Printf("  ← %s:%d  %s\n", h.Path, h.Line, h.Text)
	}
	if parentRef != nil {
		fmt.Printf("subtask of:\n  %s %s  %s\n", glyphSubtaskOf(), parentRef.ShortID, parentRef.Title)
	}
	if len(children) > 0 {
		fmt.Println("subtasks:")
		for _, c := range children {
			fmt.Printf("  %s %s  %s\n", glyphChild(), c.ShortID, c.Title)
		}
	}
	if len(blocksR) > 0 {
		fmt.Println("blocks:")
		for _, b := range blocksR {
			fmt.Printf("  %s %s  %s\n", glyphBlocks(), b.ShortID, b.Title)
		}
	}
	if len(blockedBy) > 0 {
		fmt.Println("blocked by:")
		for _, b := range blockedBy {
			fmt.Printf("  %s %s  %s\n", glyphBlockedBy(), b.ShortID, b.Title)
		}
	}
	if discFrom != nil {
		fmt.Printf("discovered from:\n  %s %s  %s\n", glyphDiscFrom(), discFrom.ShortID, discFrom.Title)
	}
	if len(discHere) > 0 {
		fmt.Println("discovered here:")
		for _, o := range discHere {
			fmt.Printf("  %s %s  %s\n", glyphDiscHere(), o.ShortID, o.Title)
		}
	}
	return 0
}

// cmdLog prints completed tasks newest-first (the Logbook, on the CLI) so an AI
// session can recall "what we recently finished". Reuses task.CompletedSince —
// the same domain rule the TUI Logbook uses.
func cmdLog(args []string) int {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	since := fs.String("since", "", "only completions on/after YYYY-MM-DD")
	days := fs.Int("days", 0, "only completions in the last N days")
	source := fs.String("source", "", "filter by source (e.g. claude)")
	asJSON := fs.Bool("json", false, "machine-readable output")

	flags, _ := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}

	bound := *since
	if *days > 0 {
		if cut := time.Now().AddDate(0, 0, -*days).Format("2006-01-02"); bound == "" || cut > bound {
			bound = cut
		}
	}
	done := task.CompletedSince(d.Tasks(), bound)
	if *source != "" {
		kept := done[:0]
		for _, t := range done {
			if t.Source() == *source {
				kept = append(kept, t)
			}
		}
		done = kept
	}

	if *asJSON {
		return printJSON(tasksToJSON(done, indexMap(d.Tasks())))
	}
	if len(done) == 0 {
		fmt.Println("no completed tasks")
		return 0
	}
	day := ""
	for _, t := range done {
		if t.Completed != day {
			day = t.Completed
			label := day
			if label == "" {
				label = "(no completion date)"
			}
			fmt.Printf("\n%s\n", label)
		}
		src := ""
		if s := t.Source(); s != "" {
			src = "  (" + s + ")" // @tags/+project are already inline in Text
		}
		fmt.Printf("  %s %s  %s%s\n", glyphDone(), shortID(t.ID()), t.Text, src)
	}
	return 0
}

// cmdReview is the weekly-review digest (T7): it surfaces what needs attention —
// overdue tasks, ones that have languished, ones with no due date, and projects
// where every open task is blocked (no next action). Read-only.
func cmdReview(args []string) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	staleDays := fs.Int("stale", 14, "flag open tasks older than N days as stale")
	asJSON := fs.Bool("json", false, "machine-readable output")
	flags, _ := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}
	all := d.Tasks()
	idx := indexMap(all)
	blocked := task.BlockedIDs(all)
	today := mutate.Today()

	// Shared triage (also powers the web /review) so the buckets never drift.
	rev := task.BuildReview(all, blocked, *staleDays, today)
	overdue, stale, undated, stuck := rev.Overdue, rev.Stale, rev.Undated, rev.StuckProjects

	if *asJSON {
		return printJSON(reviewJSON{
			Overdue:       tasksToJSON(overdue, idx),
			Stale:         tasksToJSON(stale, idx),
			Undated:       tasksToJSON(undated, idx),
			StuckProjects: stuck,
		})
	}

	if len(overdue)+len(stale)+len(undated)+len(stuck) == 0 {
		fmt.Println("review: nothing needs attention — you're on top of it " + glyphReviewClear())
		return 0
	}
	section := func(title string, ts []*task.Task) {
		if len(ts) == 0 {
			return
		}
		view.SortTasks(ts, "urgency")
		fmt.Printf("\n%s (%d)\n", title, len(ts))
		for _, t := range ts {
			fmt.Println("  " + formatRow(t, idx[t], blocked[t.ID()] && !t.Done))
		}
	}
	section("Overdue", overdue)
	section(fmt.Sprintf("Stale — open ≥ %dd, no progress", *staleDays), stale)
	section("No due date — schedule or drop", undated)
	if len(stuck) > 0 {
		fmt.Printf("\nStuck projects — every open task is blocked (%d)\n", len(stuck))
		for _, p := range stuck {
			fmt.Println("  +" + p)
		}
	}
	return 0
}

type reviewJSON struct {
	Overdue       []taskJSON `json:"overdue"`
	Stale         []taskJSON `json:"stale"`
	Undated       []taskJSON `json:"undated"`
	StuckProjects []string   `json:"stuckProjects"`
}
