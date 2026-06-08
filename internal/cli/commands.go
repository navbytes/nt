package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/quickadd"
	"github.com/navbytes/nt/internal/search"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/tui"
	"github.com/navbytes/nt/internal/web"
)

// cmdWeb starts the localhost notes viewer (SPEC §12.1). Read-only browse/render
// with mermaid; binds 127.0.0.1 by default.
func cmdWeb(args []string) int {
	// Config [web] port/host set the flag defaults; an explicit --port/--host wins.
	cfg := loadConfig()
	defPort, defHost := web.DefaultPort, "127.0.0.1"
	if cfg.WebPort != 0 {
		defPort = cfg.WebPort
	}
	if cfg.WebHost != "" {
		defHost = cfg.WebHost
	}
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	port := fs.Int("port", defPort, fmt.Sprintf("port to listen on (%d by default; falls back to a free one if taken; 0 = always pick a free one)", defPort))
	host := fs.String("host", defHost, "bind address (localhost only by default)")
	edit := fs.Bool("edit", false, "allow editing notes in the browser (default: read-only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := web.Serve(Version, fmt.Sprintf("%s:%d", *host, *port), *edit); err != nil {
		return fail(err)
	}
	return 0
}

// buildText assembles a task description from a title plus tags, project, and
// an optional note link, as inline todo.txt tokens.
func buildText(title string, tags []string, project, noteSlug string) string {
	parts := []string{title}
	for _, t := range tags {
		parts = append(parts, "@"+t)
	}
	if project != "" {
		parts = append(parts, "+"+project)
	}
	if noteSlug != "" {
		parts = append(parts, "[["+noteSlug+"]]")
	}
	return strings.Join(parts, " ")
}

func cmdAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	var tags stringSlice
	pri := fs.String("pri", "", "priority high|med|low")
	due := fs.String("due", "", "due date")
	project := fs.String("project", "", "project")
	source := fs.String("source", "cli", "origin")
	parent := fs.String("parent", "", "parent task id")
	blocks := fs.String("blocks", "", "blocks task id")
	discovered := fs.String("discovered-from", "", "task this was discovered while working on")
	recur := fs.String("recur", "", "recurrence: weekly|3d|… (prefix + for strict, e.g. +monthly)")
	noteSlug := fs.String("note", "", "link to a note slug")
	fs.Var(&tags, "tag", "tag (repeatable)")

	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	title := strings.Join(positional, " ")
	if strings.TrimSpace(title) == "" {
		return fail(fmt.Errorf("add: a title is required"))
	}
	p, ok := parsePriority(*pri)
	if !ok {
		return fail(fmt.Errorf("add: invalid priority %q", *pri))
	}
	dueVal, ok := parseDate(*due)
	if !ok {
		return fail(fmt.Errorf("add: invalid due date %q", *due))
	}

	e, ok2 := engine()
	if !ok2 {
		return 1
	}
	var created *task.Task
	err := e.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
		t := quickadd.New(buildText(title, tags, *project, *noteSlug))
		if p != 0 {
			t.SetPriority(p)
		}
		if dueVal != "" {
			t.SetKey("due", dueVal)
		}
		t.SetKey("src", *source)
		if *recur != "" {
			t.SetKey("rec", *recur)
		}
		if *parent != "" {
			if pt, amb := d.Resolve(*parent); pt != nil && !amb {
				t.SetKey("parent", pt.ID())
			}
		}
		if *blocks != "" {
			if bt, amb := d.Resolve(*blocks); bt != nil && !amb {
				t.SetKey("blocks", bt.ID())
			}
		}
		if *discovered != "" {
			if dt, amb := d.Resolve(*discovered); dt != nil && !amb {
				t.SetKey("discovered", dt.ID())
			}
		}
		d.Append(t)
		rec.Added(t)
		created = t
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("added %s  %s\n", shortID(created.ID()), title)
	return 0
}

// cmdSkip advances one or more recurring tasks to their next occurrence without
// completing them — "not this time, but keep the cadence."
func cmdSkip(args []string) int {
	if len(args) == 0 {
		return fail(fmt.Errorf("skip: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	moved := 0
	err := e.Apply("skip", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, h := range args {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("skip: %w", err)
			}
			if t.Recur() == "" {
				return fmt.Errorf("skip: %s is not a recurring task", shortID(t.ID()))
			}
			next := task.AdvanceDue(t, mutate.Today())
			if next == "" {
				return fmt.Errorf("skip: %s has an unparseable recurrence %q", shortID(t.ID()), t.Recur())
			}
			rec.Before(t)
			t.SetKey("due", next)
			moved++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("skipped to next occurrence (%d)\n", moved)
	return 0
}

func cmdNote(args []string) int {
	fs := flag.NewFlagSet("note", flag.ContinueOnError)
	var tags stringSlice
	body := fs.String("body", "", "note body")
	source := fs.String("source", "cli", "origin")
	folder := fs.String("folder", "", "subfolder under notes/ (e.g. work or work/auth)")
	var fields stringSlice
	fs.Var(&tags, "tag", "tag (repeatable)")
	fs.Var(&fields, "field", "extra frontmatter key=value (repeatable, e.g. status=stable)")

	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	title := strings.Join(positional, " ")
	fold := *folder
	// Path-style shorthand: `nt note "work/Auth design"` files it under work/
	// when no explicit --folder was given.
	if fold == "" {
		if i := strings.LastIndex(title, "/"); i >= 0 {
			fold = strings.TrimSpace(title[:i])
			title = strings.TrimSpace(title[i+1:])
		}
	}
	if strings.TrimSpace(title) == "" {
		return fail(fmt.Errorf("note: a title is required"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	n, err := note.Create(e.S, title, *body, tags, *source, fold)
	if err != nil {
		return fail(err)
	}
	if len(fields) > 0 { // --field key=value → extra frontmatter, preserved verbatim
		for _, f := range fields {
			k, v, found := strings.Cut(f, "=")
			if !found || strings.TrimSpace(k) == "" {
				return fail(fmt.Errorf("note: --field must be key=value, got %q", f))
			}
			n.Extra = append(n.Extra, strings.TrimSpace(k)+": "+strings.TrimSpace(v))
		}
		if err := n.Save(); err != nil {
			return fail(err)
		}
	}
	rel, _ := filepath.Rel(e.S.Dir, n.Path)
	fmt.Printf("note %s  %s\n", shortID(n.ID), rel)
	return 0
}

// cmdJournal opens today's daily note (notes/journal/YYYY-MM-DD.md) in $EDITOR,
// creating it if missing — the journal's CLI entry point, mirroring the web
// /journal route so an agent or a person can keep a dated log from either surface.
func cmdJournal(args []string) int {
	fs := flag.NewFlagSet("journal", flag.ContinueOnError)
	dateFlag := fs.String("date", "", "day to open (YYYY-MM-DD or today|fri|+1d; default today)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	date := mutate.Today()
	if *dateFlag != "" {
		d, ok := parseDate(*dateFlag)
		if !ok || d == "" {
			return fail(fmt.Errorf("journal: invalid date %q", *dateFlag))
		}
		date = dateparse.DatePart(d) // ignore any time-of-day
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	// Reuse an existing entry if present, else create journal/<date>.md.
	want := "journal/" + date + ".md"
	notes, _ := note.List(e.S)
	for _, n := range notes {
		if n.Rel == want {
			return runEditor(n.Path)
		}
	}
	n, err := note.Create(e.S, date, "", nil, "cli", "journal")
	if err != nil {
		return fail(err)
	}
	return runEditor(n.Path)
}

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

func cmdDone(args []string) int {
	if len(args) == 0 {
		return fail(fmt.Errorf("done: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	count := 0
	err := e.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, h := range args {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("done: %w", err)
			}
			mutate.Complete(d, rec, t, mutate.Today()) // spawns next if recurring
			count++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("done (%d)\n", count)
	return 0
}

func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	status := fs.String("status", "", "open|doing|blocked|done")
	pri := fs.String("pri", "", "priority")
	due := fs.String("due", "", "due date")
	recur := fs.String("recur", "", "recurrence (stored; Phase 3)")
	parent := fs.String("parent", "", "parent id")
	blocks := fs.String("blocks", "", "blocks id")

	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return fail(fmt.Errorf("update: need a task id"))
	}
	handles := positional // bulk: apply the same changes to every id given

	// Validate parseable flags before locking.
	var priByte byte
	if *pri != "" {
		b, ok := parsePriority(*pri)
		if !ok {
			return fail(fmt.Errorf("update: invalid priority %q", *pri))
		}
		priByte = b
	}
	var dueVal string
	if *due != "" {
		v, ok := parseDate(*due)
		if !ok {
			return fail(fmt.Errorf("update: invalid due %q", *due))
		}
		dueVal = v
	}

	e, ok := engine()
	if !ok {
		return 1
	}
	count := 0
	err := e.Apply("update", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, handle := range handles {
			t, err := resolveHandle(d, handle)
			if err != nil {
				return fmt.Errorf("update: %w", err)
			}
			rec.Before(t)
			switch *status {
			case "":
			case "done":
				mutate.Complete(d, rec, t, mutate.Today()) // spawns next if recurring
			case "open":
				t.SetDone(false, "")
				t.SetState("open")
			case "doing", "blocked":
				t.SetState(*status)
			default:
				return fmt.Errorf("update: invalid status %q", *status)
			}
			if *pri != "" {
				t.SetPriority(priByte)
			}
			if *due != "" {
				t.SetKey("due", dueVal)
			}
			if *recur != "" {
				t.SetKey("rec", *recur)
			}
			if *parent != "" {
				if pt, amb := d.Resolve(*parent); pt != nil && !amb {
					t.SetKey("parent", pt.ID())
				}
			}
			if *blocks != "" {
				if bt, amb := d.Resolve(*blocks); bt != nil && !amb {
					t.SetKey("blocks", bt.ID())
				}
			}
			count++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if count == 1 {
		fmt.Println("updated")
	} else {
		fmt.Printf("updated %d\n", count)
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

// cmdRm permanently removes tasks (journaled, so `nt undo` restores them).
func cmdRm(args []string) int {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	force := fs.Bool("force", false, "delete a note even if other notes link to it")
	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return fail(fmt.Errorf("rm: need a task id or note handle"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	// Notes delete via the trash; the rest fall through to the task engine.
	notes, _ := note.List(e.S)
	var taskHandles []string
	for _, h := range positional {
		if n, err := resolveNote(notes, h); err == nil {
			if code := rmNote(e, n, *force); code != 0 {
				return code
			}
		} else {
			taskHandles = append(taskHandles, h)
		}
	}
	if len(taskHandles) == 0 {
		return 0
	}
	count := 0
	err := e.Apply("delete", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, h := range taskHandles {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("rm: %w", err)
			}
			before := t.Line()
			d.Remove(t.ID())
			rec.Removed(t.ID(), before)
			count++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("removed %d (nt undo to restore)\n", count)
	return 0
}

func cmdDefault() int {
	e, ok := engine()
	if !ok {
		return 1
	}
	if e.S.IsFresh() {
		onboard(e)
	}
	if err := tui.Run(); err != nil {
		return fail(err)
	}
	return 0
}

// onboard seeds a sample task + note on first run and prints the essentials.
func onboard(e *mutate.Engine) {
	_ = e.Apply("seed", func(d *task.Doc, rec *mutate.Recorder) error {
		t := task.New("welcome to nt — press done when you've read this @nt")
		t.SetKey("src", "nt")
		d.Append(t)
		rec.Added(t)
		return nil
	})
	_, _ = note.Create(e.S, "Welcome to nt",
		"nt stores tasks in tasks.txt (todo.txt format) and notes here as markdown.\n\nTry:\n- `nt add \"my first task\" --due today`\n- `nt recall` to read items back later\n", []string{"nt"}, "nt", "")
	fmt.Printf(`Welcome to nt. Your store is %s

  nt add "title"   add a task
  nt               list your tasks
  nt recall        read items back (great for AI sessions)

`, e.S.Dir)
}

// --- shared helpers ------------------------------------------------------

func keep(t *task.Task, status, tag, project string, all, showBlocked bool, blocked map[string]bool) bool {
	isBlocked := blocked[t.ID()] && !t.Done
	if status != "" {
		if task.EffectiveStatus(t, isBlocked) != status {
			return false
		}
	} else {
		if !all && t.Done {
			return false
		}
		if !all && !showBlocked && isBlocked {
			return false // dependency-blocked tasks hide from the default list
		}
	}
	if tag != "" && !contains(t.Tags(), tag) {
		return false
	}
	if project != "" && !contains(t.Projects(), project) {
		return false
	}
	return true
}

func sortTasks(rows []*task.Task, by string) {
	switch by {
	case "urgency":
		task.SortByUrgency(rows)
	case "due":
		sort.SliceStable(rows, func(i, j int) bool { return dueKey(rows[i]) < dueKey(rows[j]) })
	case "created":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created < rows[j].Created })
	}
}

func dueKey(t *task.Task) string {
	if d := t.Due(); d != "" {
		return d
	}
	return "9999-99-99" // no due date sorts last
}

func formatRow(t *task.Task, idx int, isBlocked bool) string {
	icon := "○"
	switch task.EffectiveStatus(t, isBlocked) {
	case "done":
		icon = "✓"
	case "doing":
		icon = "◐"
	case "blocked":
		icon = "⊘"
	}
	var meta []string
	if t.Priority != 0 {
		meta = append(meta, "("+string(t.Priority)+")")
	}
	if d := t.Due(); d != "" {
		meta = append(meta, "due:"+d)
	}
	tail := ""
	if len(meta) > 0 {
		tail = "  " + strings.Join(meta, " ")
	}
	return fmt.Sprintf("%3d  %s %s  %s%s", idx, icon, shortID(t.ID()), t.Text, tail)
}

func indexMap(tasks []*task.Task) map[*task.Task]int {
	m := make(map[*task.Task]int, len(tasks))
	for i, t := range tasks {
		m[t] = i + 1
	}
	return m
}

// extractLinks pulls [[target]] references out of a note body.
func extractLinks(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, "[[")
		if i < 0 {
			break
		}
		j := strings.Index(rest[i+2:], "]]")
		if j < 0 {
			break
		}
		out = append(out, rest[i+2:i+2+j])
		rest = rest[i+2+j+2:]
	}
	return out
}

func runEditor(path string) int {
	// Precedence: config [defaults] editor → $EDITOR → vi.
	ed := loadConfig().Editor
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	cmd := exec.Command(ed, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fail(fmt.Errorf("editor: %w", err))
	}
	return 0
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- JSON output ---------------------------------------------------------

type taskJSON struct {
	Index     int      `json:"index"`
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Status    string   `json:"status"`
	Priority  string   `json:"priority,omitempty"`
	Due       string   `json:"due,omitempty"`
	Completed string   `json:"completed,omitempty"`
	Project   string   `json:"project,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Source    string   `json:"source,omitempty"`
	Links     []string `json:"links,omitempty"`
}

func tasksToJSON(tasks []*task.Task, idx map[*task.Task]int) []taskJSON {
	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		j := taskJSON{
			Index:     idx[t],
			ID:        t.ID(),
			Text:      t.Text,
			Status:    t.Status(),
			Due:       t.Due(),
			Completed: t.Completed,
			Tags:      t.Tags(),
			Source:    t.Source(),
			Links:     t.Links(),
		}
		if t.Priority != 0 {
			j.Priority = string(t.Priority)
		}
		if p := t.Projects(); len(p) > 0 {
			j.Project = p[0]
		}
		out = append(out, j)
	}
	return out
}

type noteJSON struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Tags    []string `json:"tags,omitempty"`
	Source  string   `json:"source,omitempty"`
	Created string   `json:"created,omitempty"`
	Body    string   `json:"body,omitempty"`
	Path    string   `json:"path"`
}

func notesToJSON(notes []*note.Note) []noteJSON {
	out := make([]noteJSON, 0, len(notes))
	for _, n := range notes {
		// Include the body: an agent recalling a note needs the finding itself,
		// not just its title (Product #4 — "the most valuable memory is the body").
		out = append(out, noteJSON{
			ID: n.ID, Title: n.Title, Tags: n.Tags, Source: n.Source,
			Created: n.Created, Body: strings.TrimSpace(n.Body), Path: n.Path,
		})
	}
	return out
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(err)
	}
	return 0
}
