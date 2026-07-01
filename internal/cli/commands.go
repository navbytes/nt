package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/quickadd"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/tui"
	"github.com/navbytes/nt/internal/web"
	"github.com/navbytes/nt/internal/workstream"
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
	detach := fs.Bool("detach", false, "run the server in the background (manage with --status / --stop)")
	stop := fs.Bool("stop", false, "stop the backgrounded server")
	status := fs.Bool("status", false, "report whether a backgrounded server is running")
	detached := fs.Bool(detachedFlag, false, "") // internal: set on the re-exec'd child
	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch {
	case *stop:
		return webStop()
	case *status:
		return webStatus()
	case *detach:
		return webDetach(*host, *port)
	}

	// The detached child records its real URL in the PID file once it's bound.
	var onReady func(string)
	if *detached {
		onReady = func(url string) {
			_ = writeWebProc(&webProc{PID: os.Getpid(), URL: url, Started: time.Now().Format(time.RFC3339)})
		}
	}
	if err := web.Serve(Version, fmt.Sprintf("%s:%d", *host, *port), onReady); err != nil {
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
	// Config [defaults] priority/source set the flag defaults; explicit flags win.
	cfg := loadConfig()
	defPri, defSource := "", "cli"
	if cfg.DefaultPriority != "" {
		defPri = cfg.DefaultPriority
	}
	if cfg.DefaultSource != "" {
		defSource = cfg.DefaultSource
	}
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	var tags stringSlice
	pri := fs.String("pri", defPri, "priority high|med|low")
	due := fs.String("due", "", "due date")
	project := fs.String("project", "", "project")
	source := fs.String("source", defSource, "origin")
	parent := fs.String("parent", "", "parent task id")
	blocks := fs.String("blocks", "", "blocks task id")
	discovered := fs.String("discovered-from", "", "task this was discovered while working on")
	recur := fs.String("recur", "", "recurrence: weekly|3d|… (prefix + for strict, e.g. +monthly)")
	noteSlug := fs.String("note", "", "link to a note slug")
	est := fs.String("est", "", "time estimate (90m, 2h, 1h30m)")
	asJSON := fs.Bool("json", false, "print the created task as JSON (id, text, status, …)")
	fs.Var(&tags, "tag", "tag (repeatable)")

	flags, positional := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	title := strings.Join(positional, " ")
	if strings.TrimSpace(title) == "" {
		return usageErr(fmt.Errorf("add: a title is required"))
	}
	var p byte
	if *pri != "" {
		b, ok := parsePriority(*pri)
		if !ok {
			return usageErr(fmt.Errorf("add: invalid priority %q", *pri))
		}
		p = b
	}
	var dueVal string
	if *due != "" {
		v, ok := parseDate(*due)
		if !ok {
			return usageErr(fmt.Errorf("add: invalid due date %q", *due))
		}
		dueVal = v
	}
	estVal := ""
	if *est != "" {
		m, ok := dateparse.Duration(*est)
		if !ok {
			return usageErr(fmt.Errorf("add: invalid estimate %q", *est))
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
		// Stamp the workstream when NT_WORKSTREAM is set, so a human's CLI writes
		// isolate the same way an agent's MCP writes do (symmetric; unset = the
		// shared backlog, unchanged). "*" is a read-only widener, never stamped.
		if ws := workstream.Env(); ws != "" && ws != "*" {
			t.SetKey("ws", ws)
		}
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
	// Non-blocking overlap warning: two agents on a shared store often create the
	// same implementation task for one decision (the field study's task-layer
	// duplication). Unlike notes, tasks legitimately repeat, so we warn rather than
	// refuse — surfacing near-duplicate tasks and any decision note on the topic.
	warnDuplicateTask(e, created)
	if *asJSON {
		// Same id-bearing shape as `list/ready --json` (and the MCP nt_add tool),
		// so an agent on the CLI path can capture-then-reference.
		return printJSON(tasksToJSON([]*task.Task{created}, nil)[0])
	}
	fmt.Printf("added %s  %s\n", shortID(created.ID()), title)
	return 0
}

// taskProjTagOverlap reports whether two tasks share a project or tag.
func taskProjTagOverlap(a, b *task.Task) bool {
	for _, p := range a.Projects() {
		if contains(b.Projects(), p) {
			return true
		}
	}
	for _, tg := range a.Tags() {
		if contains(b.Tags(), tg) {
			return true
		}
	}
	return false
}

// warnDuplicateTask prints (to stderr, non-blocking) any existing open task that
// looks like a duplicate of the one just created — shared project/tag + similar
// title — and any active decision note on the same topic, so the author can link
// or dedupe instead of quietly doubling the work.
func warnDuplicateTask(e *mutate.Engine, created *task.Task) {
	if created == nil {
		return
	}
	title := created.Display()
	d, err := e.Read()
	if err != nil {
		return
	}
	for _, t := range d.Tasks() {
		if t.ID() == created.ID() || t.Done {
			continue
		}
		if taskProjTagOverlap(created, t) && note.TitleOverlap(title, t.Display()) >= 0.5 {
			fmt.Fprintf(os.Stderr, "note: similar task already exists — %s  %s (link or dedupe instead of doubling work)\n", shortID(t.ID()), t.Display())
		}
	}
	tags := created.Tags()
	if len(tags) == 0 {
		return
	}
	for _, n := range note.Active(mustNotes(e)) {
		if n.Reserved() {
			continue
		}
		shared := false
		for _, tg := range tags {
			if contains(n.Tags, tg) {
				shared = true
				break
			}
		}
		if shared && note.TitleOverlap(title, n.Title) >= 0.5 {
			fmt.Fprintf(os.Stderr, "note: a decision note already covers this — %s  %s (consider [[linking]] the task to it)\n", shortID(n.ID), n.Title)
		}
	}
}

// recurNote renders the "next occurrence" suffix for a completion line when a
// recurring task spawned a fresh occurrence (nil ⇒ empty string, no suffix).
func recurNote(spawned *task.Task) string {
	if spawned == nil {
		return ""
	}
	if due := spawned.Due(); due != "" {
		return fmt.Sprintf(" — next occurrence %s due:%s", shortID(spawned.ID()), due)
	}
	return fmt.Sprintf(" — next occurrence %s", shortID(spawned.ID()))
}

// cmdSkip advances one or more recurring tasks to their next occurrence without
// completing them — "not this time, but keep the cadence."
func cmdSkip(args []string) int {
	handles, herr := handleArgs("skip", args)
	if herr != nil {
		return usageErr(herr)
	}
	if len(handles) == 0 {
		return usageErr(fmt.Errorf("skip: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	var lines []string
	err := e.Apply("skip", func(d *task.Doc, rec *mutate.Recorder) error {
		lines = nil
		for _, h := range handles {
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
			lines = append(lines, fmt.Sprintf("skipped %s  %s  due:%s", shortID(t.ID()), t.Text, next))
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if len(lines) == 1 {
		fmt.Println(lines[0])
	} else {
		for _, l := range lines {
			fmt.Println(l)
		}
		fmt.Printf("skipped %d\n", len(lines))
	}
	return 0
}

// cmdStart begins time-tracking a task: stamps started: (now, unix seconds) and
// sets s:doing. Pair with `nt stop` to log the elapsed time into spent: (T6).
func cmdStart(args []string) int {
	handles, herr := handleArgs("start", args)
	if herr != nil {
		return usageErr(herr)
	}
	if len(handles) == 0 {
		return usageErr(fmt.Errorf("start: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	var single string
	n := 0
	err := e.Apply("start", func(d *task.Doc, rec *mutate.Recorder) error {
		n, single = 0, ""
		for _, h := range handles {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("start: %w", err)
			}
			// Remember the pre-start state so `nt stop` can restore it (T6).
			rec.Before(t)
			if prev := t.Status(); prev != "" && prev != "doing" {
				t.SetKey("prestart", prev)
			}
			t.SetKey("started", now)
			t.SetState("doing")
			single = fmt.Sprintf("started %s  %s — `nt stop` to log the time", shortID(t.ID()), t.Text)
			n++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if n == 1 {
		fmt.Println(single)
	} else {
		fmt.Printf("started %d — `nt stop` to log the time\n", n)
	}
	return 0
}

// cmdStop ends time-tracking: adds the elapsed time since started: into spent:,
// clears started:, and returns the task to s:open (T6).
func cmdStop(args []string) int {
	handles, herr := handleArgs("stop", args)
	if herr != nil {
		return usageErr(herr)
	}
	if len(handles) == 0 {
		return usageErr(fmt.Errorf("stop: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	now := time.Now().Unix()
	var lines []string
	err := e.Apply("stop", func(d *task.Doc, rec *mutate.Recorder) error {
		lines = nil
		for _, h := range handles {
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
			// Restore the state the task was in before `nt start` set it to doing;
			// default to open when there's nothing recorded.
			restore := t.Key("prestart")
			if restore == "" {
				restore = "open"
			}
			t.SetKey("prestart", "")
			t.SetState(restore)
			lines = append(lines, fmt.Sprintf("stopped %s — logged %s (total spent %s)",
				shortID(t.ID()), dateparse.FmtDuration(elapsed), dateparse.FmtDuration(total)))
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return 0
}

func cmdNote(args []string) int {
	fs := flag.NewFlagSet("note", flag.ContinueOnError)
	var tags stringSlice
	body := fs.String("body", "", "note body")
	source := fs.String("source", "cli", "origin")
	folder := fs.String("folder", "", "subfolder under notes/ (e.g. work or work/auth)")
	desc := fs.String("description", "", "one-line summary shown in `nt index`")
	supersede := fs.String("supersede", "", "mark this note as replacing an existing one (its handle) — the old note retires from active views")
	force := fs.Bool("force", false, "create even if a near-duplicate note already exists")
	var fields stringSlice
	asJSON := fs.Bool("json", false, "print the created note as JSON (id, title, path, …)")
	fs.Var(&tags, "tag", "tag (repeatable)")
	fs.Var(&fields, "field", "extra frontmatter key=value (repeatable, e.g. status=stable)")

	flags, positional := splitArgs(args, map[string]bool{"json": true, "force": true})
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
		return usageErr(fmt.Errorf("note: a title is required"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	// Dedup-on-write guard: don't silently fork a decision a teammate already
	// captured. Skipped when --force, or when --supersede is explicitly replacing.
	if !*force && strings.TrimSpace(*supersede) == "" {
		if sim := note.FindSimilar(note.Active(mustNotes(e)), title, tags); len(sim) > 0 {
			fmt.Fprintf(os.Stderr, "note: a near-duplicate already exists — not creating. Did you mean to update it?\n")
			for _, s := range sim {
				fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", shortID(s.ID), s.Rel, s.Title)
			}
			fmt.Fprintf(os.Stderr, "→ edit it (nt edit <id>), replace it (nt note … --supersede <id>), or force a new one (--force)\n")
			return 1
		}
	}
	n, err := note.Create(e.S, title, *body, tags, *source, fold)
	if err != nil {
		return fail(err)
	}
	if h := strings.TrimSpace(*supersede); h != "" {
		if code := markSuperseded(e, h, n.ID); code != 0 {
			return code
		}
	}
	if d := strings.TrimSpace(*desc); d != "" { // --description → a modeled frontmatter key
		fields = append(fields, "description="+d)
	}
	if len(fields) > 0 { // --field key=value → extra frontmatter, preserved verbatim
		for _, f := range fields {
			k, v, found := strings.Cut(f, "=")
			if !found || strings.TrimSpace(k) == "" {
				return usageErr(fmt.Errorf("note: --field must be key=value, got %q", f))
			}
			n.Extra = append(n.Extra, strings.TrimSpace(k)+": "+strings.TrimSpace(v))
		}
		if err := n.Save(); err != nil {
			return fail(err)
		}
	}
	// Warn (don't fail) on any [[link]] in the body that doesn't resolve, so a
	// mistyped outbound reference is caught at capture instead of later by doctor.
	warnDanglingLinks(e, n)
	if *asJSON {
		return printJSON(notesToJSON([]*note.Note{n})[0])
	}
	rel, _ := filepath.Rel(e.S.Dir, n.Path)
	fmt.Printf("note %s  %s\n", shortID(n.ID), rel)
	return 0
}

// markSuperseded stamps oldHandle's note with superseded_by=newID (retiring it
// from active views). Returns a nonzero CLI code on failure.
func markSuperseded(e *mutate.Engine, oldHandle, newID string) int {
	notes := mustNotes(e)
	old, err := resolveNote(notes, oldHandle)
	if err != nil {
		return fail(fmt.Errorf("supersede: %w", err))
	}
	if old.ID == newID {
		return fail(fmt.Errorf("supersede: a note can't supersede itself"))
	}
	old.SupersededBy = newID
	if err := old.Save(); err != nil {
		return fail(err)
	}
	return 0
}

// warnDanglingLinks prints a warning for each [[link]] in the note body that
// doesn't resolve — a write-time catch for mistyped outbound references.
func warnDanglingLinks(e *mutate.Engine, n *note.Note) {
	if !strings.Contains(n.Body, "[[") {
		return
	}
	notes := mustNotes(e)
	d, _ := e.Read()
	for _, raw := range links.Wikilinks(n.Body) {
		if _, ok := links.Resolve(raw, d, notes); !ok {
			fmt.Fprintf(os.Stderr, "note: warning — [[%s]] doesn't resolve to any note or task (dangling link)\n", raw)
		}
	}
}

// cmdSupersede marks one note as replaced by another: `nt supersede <old> --by
// <new>`. The old note retires from active views (index/search/status) so a
// resume sees only the current decision, while superseded_by preserves the trail.
func cmdSupersede(args []string) int {
	fs := flag.NewFlagSet("supersede", flag.ContinueOnError)
	by := fs.String("by", "", "handle of the note that replaces the old one (required)")
	flags, positional := splitArgs(args, map[string]bool{})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 || strings.TrimSpace(*by) == "" {
		return usageErr(fmt.Errorf("supersede: usage: nt supersede <old-handle> --by <new-handle>"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	newNote, err := resolveNote(mustNotes(e), strings.TrimSpace(*by))
	if err != nil {
		return fail(fmt.Errorf("supersede: --by: %w", err))
	}
	if code := markSuperseded(e, strings.Join(positional, " "), newNote.ID); code != 0 {
		return code
	}
	fmt.Printf("superseded — retired the old note; %s is now canonical\n", shortID(newNote.ID))
	return 0
}

// cmdRelink rewrites a wrong outbound [[link]] inside a note's body:
// `nt relink <note> <old-target> <new-target>`. `nt mv` fixes *inbound* links on
// rename; this fixes an *outbound* reference that points at the wrong (or a
// nonexistent) note — the gap the write-time dangling-link warning flags.
func cmdRelink(args []string) int {
	flags, positional := splitArgs(args, map[string]bool{})
	fs := flag.NewFlagSet("relink", flag.ContinueOnError)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) < 3 {
		return usageErr(fmt.Errorf("relink: usage: nt relink <note> <old-target> <new-target>"))
	}
	handle, oldT, newT := positional[0], positional[1], positional[2]
	e, ok := engine()
	if !ok {
		return 1
	}
	notes := mustNotes(e)
	n, err := resolveNote(notes, handle)
	if err != nil {
		return fail(fmt.Errorf("relink: %w", err))
	}
	if _, ok := links.Resolve(newT, nil, notes); !ok {
		return fail(fmt.Errorf("relink: new target [[%s]] doesn't resolve to any note", newT))
	}
	body, count := relinkBody(n.Body, oldT, newT)
	if count == 0 {
		return fail(fmt.Errorf("relink: no [[%s]] found in %s", oldT, shortID(n.ID)))
	}
	n.Body = body
	if err := n.Save(); err != nil {
		return fail(err)
	}
	fmt.Printf("relinked %d reference(s): [[%s]] → [[%s]] in %s\n", count, oldT, newT, shortID(n.ID))
	return 0
}

// relinkBody rewrites [[oldT]], [[oldT|alias]] and [[oldT#frag]] to point at newT,
// preserving any alias/fragment. Returns the new body and the replacement count.
func relinkBody(body, oldT, newT string) (string, int) {
	count := 0
	for _, suffix := range []string{"]]", "|", "#"} {
		from := "[[" + oldT + suffix
		to := "[[" + newT + suffix
		count += strings.Count(body, from)
		body = strings.ReplaceAll(body, from, to)
	}
	return body, count
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
			return usageErr(fmt.Errorf("journal: invalid date %q", *dateFlag))
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
	handles, herr := handleArgs("done", args)
	if herr != nil {
		return usageErr(herr)
	}
	if len(handles) == 0 {
		return usageErr(fmt.Errorf("done: need a task id"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	var single string
	count := 0
	err := e.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		count, single = 0, ""
		for _, h := range handles {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("done: %w", err)
			}
			spawned := completeAndSpawn(d, rec, t) // spawns next if recurring
			single = fmt.Sprintf("done %s  %s%s", shortID(t.ID()), t.Text, recurNote(spawned))
			count++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if count == 1 {
		fmt.Println(single)
	} else {
		fmt.Printf("done %d\n", count)
	}
	return 0
}

// completeAndSpawn completes t and returns the recurrence occurrence that
// mutate.Complete appended (nil when none). It detects the spawn by diffing the
// doc's task ids around the call, since mutate.Complete itself returns nothing.
func completeAndSpawn(d *task.Doc, rec *mutate.Recorder, t *task.Task) *task.Task {
	before := make(map[string]bool)
	for _, x := range d.Tasks() {
		before[x.ID()] = true
	}
	mutate.Complete(d, rec, t, mutate.Today())
	for _, x := range d.Tasks() {
		if !before[x.ID()] {
			return x
		}
	}
	return nil
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
	title := fs.String("title", "", "replace the task's description (keeps tags/project/links)")
	project := fs.String("project", "", "set the +project ('none' clears)")
	source := fs.String("source", "", "set the task's source ('none' clears)")
	asJSON := fs.Bool("json", false, "print the updated task(s) as JSON")
	var addTags, rmTags stringSlice
	fs.Var(&addTags, "tag", "add an @tag (repeatable)")
	fs.Var(&rmTags, "untag", "remove an @tag (repeatable)")

	flags, positional := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("update: need a task id"))
	}
	handles := positional // bulk: apply the same changes to every id given

	// Validate parseable flags before locking.
	var priByte byte
	if *pri != "" {
		b, ok := parsePriority(*pri)
		if !ok {
			return usageErr(fmt.Errorf("update: invalid priority %q", *pri))
		}
		priByte = b
	}
	var dueVal string
	if *due != "" {
		v, ok := parseDate(*due)
		if !ok {
			return usageErr(fmt.Errorf("update: invalid due %q", *due))
		}
		dueVal = v
	}
	estVal, estSet := "", false
	if *est != "" {
		estSet = true
		if *est != "none" && *est != "-" {
			m, ok := dateparse.Duration(*est)
			if !ok {
				return usageErr(fmt.Errorf("update: invalid estimate %q", *est))
			}
			estVal = dateparse.FmtDuration(m)
		}
	}

	switch *status {
	case "", "done", "open", "doing", "blocked":
	default:
		return usageErr(fmt.Errorf("update: invalid status %q", *status))
	}

	e, ok := engine()
	if !ok {
		return 1
	}
	count := 0
	var single, recurMsg string
	var updated []*task.Task
	err := e.Apply("update", func(d *task.Doc, rec *mutate.Recorder) error {
		count, single, recurMsg, updated = 0, "", "", nil
		for _, handle := range handles {
			t, err := resolveHandle(d, handle)
			if err != nil {
				return fmt.Errorf("update: %w", err)
			}
			rec.Before(t)
			switch *status {
			case "":
			case "done":
				recurMsg = recurNote(completeAndSpawn(d, rec, t)) // spawns next if recurring
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
			if *title != "" {
				t.SetTitle(*title)
			}
			if *project != "" {
				if *project == "none" || *project == "-" {
					t.SetProject("")
				} else {
					t.SetProject(*project)
				}
			}
			if *source != "" {
				if *source == "none" || *source == "-" {
					t.SetKey("src", "")
				} else {
					t.SetKey("src", *source)
				}
			}
			for _, tg := range addTags {
				t.AddTag(tg)
			}
			for _, tg := range rmTags {
				t.RemoveTag(tg)
			}
			single = fmt.Sprintf("updated %s  %s", shortID(t.ID()), t.Text)
			updated = append(updated, t)
			count++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		return printJSON(tasksToJSON(updated, nil))
	}
	if count == 1 {
		fmt.Println(single + recurMsg)
	} else {
		fmt.Printf("updated %d%s\n", count, recurMsg)
	}
	return 0
}

// cmdRm removes tasks (journaled, so `nt undo` restores them) and notes (to
// .trash/). Interactive callers are asked to confirm (skip with --yes); a note
// with inbound [[links]] is guarded — delete anyway (--force, leaves them
// dangling) or strip the links first (--unlink).
func cmdRm(args []string) int {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	force := fs.Bool("force", false, "delete a note even if other notes link to it (leaves dangling links)")
	unlink := fs.Bool("unlink", false, "strip inbound [[links]] to a note before deleting it")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	fs.BoolVar(yes, "y", false, "skip the confirmation prompt")
	flags, positional := splitArgs(args, map[string]bool{"force": true, "unlink": true, "yes": true, "y": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("rm: need a task id or note handle"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	// Resolve and classify EVERY handle up front (note vs task vs unknown) before
	// mutating anything, so a later invalid handle never leaves earlier notes
	// trashed (the deletion is otherwise non-atomic across the two stores).
	notes, _ := note.List(e.S)
	d, derr := e.Read()
	if derr != nil {
		return fail(derr)
	}
	var noteTargets []*note.Note
	var taskHandles, unknown []string
	for _, h := range positional {
		if n, err := resolveNote(notes, h); err == nil {
			noteTargets = append(noteTargets, n)
			continue
		}
		if _, terr := resolveHandle(d, h); terr == nil {
			taskHandles = append(taskHandles, h)
		} else if task.IsPositional(h) && !interactive() {
			// Surface the stable-id guidance directly rather than burying it as a
			// generic "unknown handle".
			return fail(fmt.Errorf("rm: %w", terr))
		} else {
			unknown = append(unknown, h)
		}
	}
	if len(unknown) > 0 {
		return fail(fmt.Errorf("rm: no task or note: %s", strings.Join(unknown, ", ")))
	}

	// Destructive deletes must be explicit for non-interactive callers: require
	// --yes (matching the cautious note-with-backlinks behavior) so an agent or
	// script can't silently wipe tasks it merely listed.
	if len(taskHandles) > 0 && !interactive() && !*yes {
		return fail(fmt.Errorf("rm: refusing to delete %d task(s) non-interactively — pass --yes to confirm", len(taskHandles)))
	}

	// Notes delete via the trash; the rest fall through to the task engine.
	for _, n := range noteTargets {
		if code := rmNote(e, n, *force, *unlink, *yes); code != 0 {
			return code
		}
	}
	if len(taskHandles) == 0 {
		return 0
	}

	// Confirm task deletion interactively (the delete is undoable regardless).
	if interactive() && !*yes {
		label := fmt.Sprintf("Delete %d task(s)?", len(taskHandles))
		if len(taskHandles) == 1 {
			label = fmt.Sprintf("Delete task %s?", taskHandles[0])
		}
		if !confirm(label) {
			fmt.Println("cancelled")
			return 0
		}
	}

	count := 0
	var single string
	err := e.Apply("delete", func(d *task.Doc, rec *mutate.Recorder) error {
		count, single = 0, ""
		for _, h := range taskHandles {
			t, err := resolveHandle(d, h)
			if err != nil {
				return fmt.Errorf("rm: %w", err)
			}
			before := t.Line()
			single = fmt.Sprintf("removed %s  %s (nt undo to restore)", shortID(t.ID()), t.Text)
			d.Remove(t.ID())
			rec.Removed(t.ID(), before)
			count++
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if count == 1 {
		fmt.Println(single)
	} else {
		fmt.Printf("removed %d (nt undo to restore)\n", count)
	}
	return 0
}

func cmdDefault() int {
	e, ok := engine()
	if !ok {
		return 1
	}
	// The no-arg TUI needs a real terminal. In any non-interactive context — a
	// pipe, CI, a container, or an AI agent shelling out (the headline use case)
	// — opening /dev/tty fails and the *most documented* first command ("just
	// run nt") would crash. Fall back to a plain task list instead, and do NOT
	// seed the welcome sample items: an agent's store should contain only what
	// the agent captured, never demo rows that pollute recall/ready/review.
	if !interactive() {
		return cmdList(nil)
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
		t := task.New("welcome to nt — press x to complete this, ? for all keys @nt")
		t.SetKey("src", "nt")
		d.Append(t)
		rec.Added(t)
		return nil
	})
	_, _ = note.Create(e.S, "Welcome to nt",
		"nt stores tasks in tasks.txt (todo.txt format) and notes here as markdown.\n\nTry:\n- `nt add \"my first task\" --due today`\n- `nt index` to see everything at a glance later\n", []string{"nt"}, "nt", "")
	fmt.Printf(`Welcome to nt. Your store is %s

  nt add "title"   add a task
  nt               list your tasks
  nt index         see everything at a glance (great for AI sessions)

`, e.S.Dir)
}

// --- shared helpers ------------------------------------------------------

// keep/sortTasks moved to internal/view (Apply/Keep/SortTasks) so the CLI, TUI,
// and web share one filter/sort implementation for lists and saved views.

func formatRow(t *task.Task, idx int, isBlocked bool) string {
	icon := iconStatus(task.EffectiveStatus(t, isBlocked))
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
