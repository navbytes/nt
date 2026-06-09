package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/quickadd"
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
	est := fs.String("est", "", "time estimate (90m, 2h, 1h30m)")
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
	estVal := ""
	if *est != "" {
		m, ok := dateparse.Duration(*est)
		if !ok {
			return fail(fmt.Errorf("add: invalid estimate %q", *est))
		}
		estVal = dateparse.FmtDuration(m)
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
		if estVal != "" {
			t.SetKey("est", estVal)
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

// cmdStart begins time-tracking a task: stamps started: (now, unix seconds) and
// sets s:doing. Pair with `nt stop` to log the elapsed time into spent: (T6).
func cmdStart(args []string) int {
	if len(args) == 0 {
		return fail(fmt.Errorf("start: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	n := 0
	err := e.Apply("start", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, h := range args {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("start: %w", err)
			}
			rec.Before(t)
			t.SetKey("started", now)
			t.SetState("doing")
			n++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("started %d task(s) — `nt stop` to log the time\n", n)
	return 0
}

// cmdStop ends time-tracking: adds the elapsed time since started: into spent:,
// clears started:, and returns the task to s:open (T6).
func cmdStop(args []string) int {
	if len(args) == 0 {
		return fail(fmt.Errorf("stop: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	now := time.Now().Unix()
	var summary string
	err := e.Apply("stop", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, h := range args {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("stop: %w", err)
			}
			startedStr := t.Key("started")
			if startedStr == "" {
				return fmt.Errorf("stop: %s isn't being tracked — `nt start` it first", shortID(t.ID()))
			}
			started, _ := strconv.ParseInt(startedStr, 10, 64)
			elapsed := int((now - started) / 60)
			if elapsed < 0 {
				elapsed = 0
			}
			prev, _ := dateparse.Duration(t.Key("spent"))
			total := prev + elapsed
			rec.Before(t)
			t.SetKey("spent", dateparse.FmtDuration(total))
			t.SetKey("started", "") // clear the running timer
			t.SetState("open")
			summary = fmt.Sprintf("stopped %s — logged %s (total spent %s)",
				shortID(t.ID()), dateparse.FmtDuration(elapsed), dateparse.FmtDuration(total))
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	fmt.Println(summary)
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
	est := fs.String("est", "", "time estimate (90m, 2h; 'none' clears)")

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
	estVal, estSet := "", false
	if *est != "" {
		estSet = true
		if *est != "none" && *est != "-" {
			m, ok := dateparse.Duration(*est)
			if !ok {
				return fail(fmt.Errorf("update: invalid estimate %q", *est))
			}
			estVal = dateparse.FmtDuration(m)
		}
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
			if estSet {
				t.SetKey("est", estVal) // "" clears it
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
	blockedByDep := blocked[t.ID()]
	if status != "" {
		if task.EffectiveStatus(t, blockedByDep && !t.Done) != status {
			return false
		}
	} else if !task.VisibleInList(t, blockedByDep, all, all || showBlocked) {
		// default list: done hides unless --all; dependency-blocked hides unless
		// --all / --show-blocked (the rule shared with the TUI/web via task).
		return false
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
	if e := t.Key("est"); e != "" {
		meta = append(meta, "est:"+e)
	}
	if sp := t.Key("spent"); sp != "" {
		meta = append(meta, "spent:"+sp)
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
