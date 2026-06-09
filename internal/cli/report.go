package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/task"
)

// Read/report commands: the non-mutating verbs that list, query, and render
// tasks/notes (list, ready, today, agenda, recall, search, links, log). Split out
// of commands.go, which keeps the mutating verbs + shared helpers (E6).

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	status := fs.String("status", "", "open|doing|blocked|done")
	tag := fs.String("tag", "", "filter by tag")
	project := fs.String("project", "", "filter by project")
	sortBy := fs.String("sort", "", "urgency|due|created")
	all := fs.Bool("all", false, "include done tasks")
	showBlocked := fs.Bool("show-blocked", false, "include dependency-blocked tasks")
	tree := fs.Bool("tree", false, "show sub-tasks indented under their parent, with progress")
	asJSON := fs.Bool("json", false, "machine-readable output")

	flags, _ := splitArgs(args, map[string]bool{"all": true, "json": true, "show-blocked": true, "tree": true})
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
	all3 := d.Tasks()
	idx := indexMap(all3)
	blocked := task.BlockedIDs(all3)

	var rows []*task.Task
	for _, t := range all3 {
		if !keep(t, *status, *tag, *project, *all, *showBlocked, blocked) {
			continue
		}
		rows = append(rows, t)
	}
	sortTasks(rows, *sortBy)

	if *asJSON {
		return printJSON(tasksToJSON(rows, idx))
	}
	if len(rows) == 0 {
		fmt.Println("no tasks")
		return 0
	}
	if *tree {
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
		if !keep(t, "", *tag, *project, false, false, blocked) {
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
	sortTasks(rows, "urgency")

	if *asJSON {
		return printJSON(tasksToJSON(rows, idx))
	}
	if len(rows) == 0 {
		fmt.Println("nothing ready — all clear, or everything open is blocked")
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
	sortTasks(rows, "urgency")

	if *asJSON {
		return printJSON(tasksToJSON(rows, idx))
	}
	if len(rows) == 0 {
		fmt.Println("nothing on the agenda — all clear within the horizon")
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

func cmdRecall(args []string) int {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	source := fs.String("source", "", "filter by source")
	since := fs.String("since", "", "only items on/after YYYY-MM-DD")
	typ := fs.String("type", "all", "task|note|all")
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
	idx := indexMap(d.Tasks())

	var tasks []*task.Task
	if *typ != "note" {
		for _, t := range d.Tasks() {
			if *source != "" && t.Source() != *source {
				continue
			}
			if *since != "" && t.Created != "" && t.Created < *since {
				continue
			}
			tasks = append(tasks, t)
		}
	}
	var notes []*note.Note
	if *typ != "task" {
		all, _ := note.List(e.S)
		for _, n := range all {
			if *source != "" && n.Source != *source {
				continue
			}
			if *since != "" && n.Created != "" && n.Created[:min(10, len(n.Created))] < *since {
				continue
			}
			notes = append(notes, n)
		}
	}

	if *asJSON {
		out := map[string]any{
			"tasks": tasksToJSON(tasks, idx),
			"notes": notesToJSON(notes),
		}
		return printJSON(out)
	}
	if len(tasks) == 0 && len(notes) == 0 {
		fmt.Println("nothing to recall")
		return 0
	}
	blocked := task.BlockedIDs(d.Tasks())
	for _, t := range tasks {
		fmt.Println(formatRow(t, idx[t], blocked[t.ID()]))
	}
	for _, n := range notes {
		fmt.Printf("   ▤ %s  %s\n", shortID(n.ID), n.Title)
	}
	return 0
}

func cmdSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	typ := fs.String("type", "all", "note|task|all")
	var tags stringSlice
	fs.Var(&tags, "tag", "only items with this tag (repeatable, AND)")
	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	query := strings.Join(positional, " ")
	if query == "" && len(tags) == 0 {
		return fail(fmt.Errorf("search: need a query or --tag"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	hasAll := func(have []string) bool {
		for _, w := range tags {
			if !contains(have, w) {
				return false
			}
		}
		return true
	}
	found := 0
	if *typ != "task" {
		notes, _ := note.List(e.S)
		if query != "" {
			tagsByPath := make(map[string][]string, len(notes))
			for _, n := range notes {
				tagsByPath[n.Path] = n.Tags
			}
			emitted := map[string]bool{}
			hits, _ := search.Literal(query, e.S.NotesDir())
			for _, h := range hits {
				if len(tags) > 0 && !hasAll(tagsByPath[h.Path]) {
					continue
				}
				rel, _ := filepath.Rel(e.S.Dir, h.Path)
				fmt.Printf("note  %s:%d  %s\n", rel, h.Line, strings.TrimSpace(h.Text))
				emitted[h.Path] = true
				found++
			}
			ql := strings.ToLower(query)
			for _, n := range notes { // title matches not caught by body search
				if emitted[n.Path] || (len(tags) > 0 && !hasAll(n.Tags)) {
					continue
				}
				if strings.Contains(strings.ToLower(n.Title), ql) {
					fmt.Printf("note  %s  %s\n", n.Rel, n.Title)
					found++
				}
			}
		} else { // tag-only listing
			for _, n := range notes {
				if hasAll(n.Tags) {
					fmt.Printf("note  %s  %s\n", n.Rel, n.Title)
					found++
				}
			}
		}
	}
	if *typ != "note" {
		d, _ := e.Read()
		idx := indexMap(d.Tasks())
		blocked := task.BlockedIDs(d.Tasks())
		needle := strings.ToLower(query)
		for _, t := range d.Tasks() {
			if len(tags) > 0 && !hasAll(t.Tags()) {
				continue
			}
			if query == "" || strings.Contains(strings.ToLower(t.Line()), needle) {
				fmt.Println(formatRow(t, idx[t], blocked[t.ID()]))
				found++
			}
		}
	}
	if found == 0 {
		fmt.Println("no matches")
	}
	return 0
}

func cmdLinks(args []string) int {
	fs := flag.NewFlagSet("links", flag.ContinueOnError)
	orphans := fs.Bool("orphans", false, "list notes with no inbound links (no handle needed)")
	flags, positional := splitArgs(args, nil)
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
		return fail(fmt.Errorf("links: need an id (or note:slug), or --orphans"))
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

	fmt.Printf("%s  %s\n", shortID(id), title)
	fmt.Println("forward:")
	if len(forward) == 0 {
		fmt.Println("  (none)")
	}
	for _, target := range forward {
		key, alias := links.NormalizeTarget(target)
		disp := key
		if alias != "" {
			disp = alias
		}
		switch it, ok := links.Resolve(target, d, notes); {
		case ok:
			fmt.Printf("  → [%s] %s  %s\n", it.Kind, shortID(it.ID), it.Title)
		case it.Kind == "ambiguous":
			fmt.Printf("  → [[%s]] (ambiguous: %s)\n", disp, it.Title)
		default:
			fmt.Printf("  → [[%s]] (unresolved)\n", disp)
		}
	}
	fmt.Println("linked from:")
	back := links.Backlinks(e.S, id, noteRel)
	if len(back) == 0 {
		fmt.Println("  (none)")
	}
	for _, h := range back {
		rel, _ := filepath.Rel(e.S.Dir, h.Path)
		fmt.Printf("  ← %s:%d  %s\n", rel, h.Line, strings.TrimSpace(h.Text))
	}

	// Typed provenance: discovered-from (this task's origin) and the reverse
	// (work discovered while this task was being done).
	if self != nil {
		if df := self.Discovered(); df != "" {
			if origin := d.FindByID(df); origin != nil {
				fmt.Printf("discovered from:\n  ↑ %s  %s\n", shortID(origin.ID()), origin.Text)
			}
		}
		var spawned []*task.Task
		for _, o := range d.Tasks() {
			if o.Discovered() == self.ID() {
				spawned = append(spawned, o)
			}
		}
		if len(spawned) > 0 {
			fmt.Println("discovered here:")
			for _, o := range spawned {
				fmt.Printf("  ↳ %s  %s\n", shortID(o.ID()), o.Text)
			}
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
		fmt.Printf("  ✓ %s  %s%s\n", shortID(t.ID()), t.Text, src)
	}
	return 0
}
