package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/aisync"
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
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	port := fs.Int("port", web.DefaultPort, fmt.Sprintf("port to listen on (%d by default; falls back to a free one if taken; 0 = always pick a free one)", web.DefaultPort))
	host := fs.String("host", "127.0.0.1", "bind address (localhost only by default)")
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
	recur := fs.String("recur", "", "recurrence: weekly|3d|…")
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

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	status := fs.String("status", "", "open|doing|blocked|done")
	tag := fs.String("tag", "", "filter by tag")
	project := fs.String("project", "", "filter by project")
	sortBy := fs.String("sort", "", "urgency|due|created")
	all := fs.Bool("all", false, "include done tasks")
	showBlocked := fs.Bool("show-blocked", false, "include dependency-blocked tasks")
	asJSON := fs.Bool("json", false, "machine-readable output")

	flags, _ := splitArgs(args, map[string]bool{"all": true, "json": true, "show-blocked": true})
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
	for _, t := range rows {
		fmt.Println(formatRow(t, idx[t], blocked[t.ID()]))
	}
	return 0
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

	var rows []*task.Task
	for _, t := range all {
		// keep with all=false, showBlocked=false drops done + dependency-blocked.
		if !keep(t, "", *tag, *project, false, false, blocked) {
			continue
		}
		if *source != "" && t.Source() != *source {
			continue
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
	handle := positional[0]

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
	err := e.Apply("update", func(d *task.Doc, rec *mutate.Recorder) error {
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
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Println("updated")
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

func cmdArchive(args []string) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	n, err := e.Archive()
	if err != nil {
		return fail(err)
	}
	if n == 0 {
		fmt.Println("nothing to archive")
		return 0
	}
	fmt.Printf("archived %d task(s) → done.txt (not undoable)\n", n)
	return 0
}

func cmdUndo(args []string) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	op, did, err := e.Undo()
	if err != nil {
		return fail(err)
	}
	if !did {
		fmt.Println("nothing to undo")
		return 0
	}
	fmt.Printf("undid: %s\n", op)
	return 0
}

func cmdEdit(args []string) int {
	if len(args) == 0 {
		return fail(fmt.Errorf("edit: need an id (or note:slug)"))
	}
	handle := args[0]
	e, ok := engine()
	if !ok {
		return 1
	}
	// Notes are single files — edit in place (safe, atomic save).
	if strings.HasPrefix(handle, "note:") {
		want := strings.TrimPrefix(handle, "note:")
		notes, _ := note.List(e.S)
		for _, n := range notes {
			if strings.TrimSuffix(filepath.Base(n.Path), ".md") == want || n.ID == want {
				return runEditor(n.Path)
			}
		}
		return fail(fmt.Errorf("edit: no note %q", want))
	}

	// Tasks: never hand $EDITOR the shared tasks.txt (SPEC §6.2). Extract the
	// line to a temp file, edit, then re-apply as a ULID-keyed op.
	d, err := e.Read()
	if err != nil {
		return fail(err)
	}
	t, rerr := resolveHandle(d, handle)
	if rerr != nil {
		return fail(fmt.Errorf("edit: %w", rerr))
	}
	id := t.ID()
	tmp, err := os.CreateTemp("", "nt-edit-*.txt")
	if err != nil {
		return fail(err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	tmp.WriteString(t.Line() + "\n")
	tmp.Close()
	if code := runEditor(tmpName); code != 0 {
		return code
	}
	edited, err := os.ReadFile(tmpName)
	if err != nil {
		return fail(err)
	}
	line := firstNonEmptyLine(string(edited))
	if line == "" {
		return fail(fmt.Errorf("edit: aborted (empty)"))
	}
	nt, okp := task.ParseLine(line)
	if !okp {
		return fail(fmt.Errorf("edit: result is not a task"))
	}
	if nt.ID() == "" {
		nt.SetKey("id", id) // preserve identity
	}
	err = e.Apply("edit", func(d *task.Doc, rec *mutate.Recorder) error {
		old := d.FindByID(id)
		if old == nil {
			return fmt.Errorf("edit: task vanished")
		}
		rec.Before(old)
		d.ReplaceByID(id, nt)
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Println("updated")
	return 0
}

func cmdPath(args []string) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	fmt.Println(e.S.Dir)
	return 0
}

// cmdMv renames or moves a note and rewrites every [[link]] to it across tasks
// and notes. dest is a new name or a folder/path (relative to notes/).
func cmdMv(args []string) int {
	if len(args) < 2 {
		return fail(fmt.Errorf("mv: usage: nt mv <note> <new-name|folder/path>"))
	}
	src, dest := args[0], strings.Join(args[1:], " ")
	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	want := strings.TrimPrefix(src, "note:")
	it, ok := links.Resolve(want, nil, notes)
	if !ok {
		if it.Kind == "ambiguous" {
			return fail(fmt.Errorf("mv: %q is ambiguous (%s) — qualify with a folder", want, it.Title))
		}
		return fail(fmt.Errorf("mv: no note %q", want))
	}
	var n *note.Note
	for _, x := range notes {
		if x.Path == it.Path {
			n = x
			break
		}
	}
	newRel, updated, err := e.RenameNote(n, notes, dest)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("renamed → %s (updated %d reference(s))\n", newRel, updated)
	return 0
}

// cmdDoctor reconciles tasks.txt after a git merge or a hand-edit: it drops
// duplicate-ULID lines (which a `merge=union` merge can leave) and assigns ids to
// any task line missing one. --check reports without fixing (exit 1 if any).
func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	check := fs.Bool("check", false, "report problems without fixing (exit 1 if any)")
	flags, _ := splitArgs(args, map[string]bool{"check": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	rep, err := e.Doctor(!*check)
	if err != nil {
		return fail(err)
	}
	for _, a := range rep.Actions {
		fmt.Println("  " + a)
	}
	if rep.Issues() == 0 {
		fmt.Println("store is healthy — no issues found")
		return 0
	}
	if *check {
		fmt.Printf("found %d issue(s): %d duplicate id(s), %d missing id(s) — run `nt doctor` to fix\n",
			rep.Issues(), rep.DupIDsRemoved, rep.IDsAssigned)
		return 1
	}
	fmt.Printf("fixed %d issue(s): %d duplicate id(s) removed, %d id(s) assigned\n",
		rep.Issues(), rep.DupIDsRemoved, rep.IDsAssigned)
	return 0
}

// cmdGitInit prepares $NT_DIR for version control: a .gitattributes so the
// append-mostly task files union-merge across branches (instead of conflicting),
// a .gitignore for machine-local/transient files, and `git init` if needed.
// Reconcile any union-merge duplicates afterwards with `nt doctor`.
func cmdGitInit(args []string) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	dir := e.S.Dir

	const attrs = "# nt: union-merge the append-mostly task files so concurrent branches\n" +
		"# don't conflict on every add; run `nt doctor` after a merge to dedup.\n" +
		"tasks.txt merge=union\n" +
		"done.txt merge=union\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(attrs), 0o644); err != nil {
		return fail(err)
	}
	const ignore = "# nt: machine-local / transient state — don't sync\n" +
		"undo.jsonl\n" +
		"tasks.txt.lock\n" +
		"nt.log\n" +
		".claude-sync.json\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignore), 0o644); err != nil {
		return fail(err)
	}

	created := false
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		cmd := exec.Command("git", "init")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fail(fmt.Errorf("git init: %v: %s", err, strings.TrimSpace(string(out))))
		}
		created = true
	}

	fmt.Printf("wrote .gitattributes + .gitignore in %s\n", dir)
	if created {
		fmt.Println("initialized a git repo there")
	} else {
		fmt.Println("(already a git repo)")
	}
	fmt.Printf("next:  (cd %s && git add -A && git commit -m \"nt store\")\n", dir)
	fmt.Println("after a merge:  nt doctor")
	return 0
}

// cmdHook reads a Claude Code PostToolUse JSON event from stdin and syncs the
// session's TodoWrite list into nt (SPEC §8). It is deliberately silent and
// always exits 0 — a hook must never break or slow the Claude session.
func cmdHook(args []string) int {
	e, err := mutate.Open()
	if err != nil {
		return 0
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 0
	}
	_ = aisync.Sync(e, data)
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
	ed := os.Getenv("EDITOR")
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
