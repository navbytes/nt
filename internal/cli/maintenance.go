package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/aisync"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// Store-maintenance and housekeeping commands: archive, undo, edit, path,
// rename/move, doctor, git-init, and the Claude Code TodoWrite hook. Split out of
// commands.go to keep that file focused on the task/note verbs (E6).

// cmdArchive does double duty: `nt archive <note…> [--undo]` retires (or
// restores) notes via a frontmatter flag, while `nt archive` with no note
// archives completed tasks to done.txt. They're the same idea — move finished
// work out of the active view — on the two entity types.
func cmdArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	undo := fs.Bool("undo", false, "unarchive the given note(s) instead")
	flags, handles := splitArgs(args, map[string]bool{"undo": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	if len(handles) > 0 {
		return archiveNotes(e, handles, *undo)
	}
	if *undo {
		return usageErr(fmt.Errorf("archive: --undo needs a note handle (task archive isn't undoable; use `nt undo`)"))
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

// archiveNotes flips the archived frontmatter flag on each note. Archived notes
// stay on disk (links intact, still greppable) but drop out of the active views
// and search — a soft, reversible retire.
func archiveNotes(e *mutate.Engine, handles []string, unarchive bool) int {
	notes, _ := note.List(e.S)
	count := 0
	for _, h := range handles {
		n, err := resolveNote(notes, h)
		if err != nil {
			return fail(fmt.Errorf("archive: %w", err))
		}
		n.Archived = !unarchive
		n.Updated = time.Now().Format(time.RFC3339)
		if err := n.Save(); err != nil {
			return fail(err)
		}
		count++
	}
	verb := "archived"
	if unarchive {
		verb = "unarchived"
	}
	fmt.Printf("%s %d note(s)\n", verb, count)
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
		return usageErr(fmt.Errorf("edit: need an id (or note:slug)"))
	}
	handle := args[0]
	e, ok := engine()
	if !ok {
		return 1
	}
	// Notes are single files — edit in place (safe, atomic save). Accept an
	// explicit note: prefix or any bare note handle (slug/title/short id), the same
	// handle every other note verb takes.
	notes, _ := note.List(e.S)
	if strings.HasPrefix(handle, "note:") {
		want := strings.TrimPrefix(handle, "note:")
		if n, nerr := resolveNote(notes, want); nerr == nil {
			return runEditor(n.Path)
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
		// Not a task — fall back to a note handle so `nt edit <slug>` works without
		// the note: prefix (the bare-handle convention the skill documents).
		if n, nerr := resolveNote(notes, handle); nerr == nil {
			return runEditor(n.Path)
		}
		return fail(fmt.Errorf("edit: no task or note %q", handle))
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
		return usageErr(fmt.Errorf("mv: usage: nt mv <note> <new-name|folder/path>"))
	}
	// dest is a single token (a name or folder/path); reject stray extra words so
	// they don't silently get joined into the filename.
	if len(args) > 2 {
		return usageErr(fmt.Errorf("mv: dest must be a single token (got %d extra); quote names with spaces", len(args)-2))
	}
	src, dest := args[0], args[1]
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
	// Notes lint: the KB-side health check (broken [[links]], plus informational
	// counts for missing descriptions and orphans). Read-only — no lock needed,
	// and never auto-fixed (a dangling link is a user decision, like a dep cycle).
	nl := lintNotes(e)
	taskProblem := rep.HasProblems()
	noteProblem := len(nl.Dangling) > 0

	if len(rep.Actions) > 0 || len(rep.Warnings) > 0 || noteProblem {
		if *check {
			fmt.Println("doctor — problems found:")
		} else {
			fmt.Println("doctor — changes:")
		}
	}
	for _, a := range rep.Actions {
		fmt.Println("  " + a)
	}
	for _, w := range rep.Warnings {
		fmt.Println("  ⚠ " + w)
	}
	for _, dl := range nl.Dangling {
		fmt.Println("  ⚠ dangling link " + dl)
	}

	if !taskProblem && !noteProblem {
		fmt.Println("store is healthy — no issues found")
		printNoteHygiene(nl)
		return 0
	}
	if rep.Issues() > 0 {
		verb := "fixed"
		if *check {
			verb = "found"
		}
		fmt.Printf("%s %d issue(s): %d duplicate id(s), %d archived-dup(s), %d missing id(s)%s\n",
			verb, rep.Issues(), rep.DupIDsRemoved, rep.CrossFileDups, rep.IDsAssigned,
			map[bool]string{true: " — run `nt doctor` to fix", false: ""}[*check])
	}
	if len(rep.Warnings) > 0 {
		fmt.Printf("%d dependency warning(s) need a manual fix (see ⚠ above)\n", len(rep.Warnings))
	}
	if noteProblem {
		fmt.Printf("%d dangling note link(s) — fix the [[target]] or the note it points to\n", len(nl.Dangling))
	}
	printNoteHygiene(nl)
	if *check {
		return 1
	}
	return 0
}

// noteLint is the KB-side health report `nt doctor` produces alongside the task
// reconciliation.
type noteLint struct {
	Dangling    []string // "[[target]] in <source>" — an unresolved wiki-link (a real break)
	NoteCount   int
	MissingDesc []string // handles of active notes with no explicit `description:`
	Orphans     []string // handles of active notes nothing links to (informational)
}

// lintNotes scans notes and tasks for KB-graph health: unresolved [[links]]
// (reported as fixable-by-hand problems), plus informational counts of notes
// missing a description and notes nothing links to. Links resolve against ALL
// notes (incl. archived) so a link to a retired note isn't miscalled dangling.
func lintNotes(e *mutate.Engine) noteLint {
	var rep noteLint
	allNotes, _ := note.List(e.S)
	active := note.Active(allNotes)
	d, _ := e.Read()

	linked := map[string]bool{}
	check := func(raw, src string) {
		if it, ok := links.Resolve(raw, d, allNotes); ok {
			if it.Kind == "note" {
				linked[it.Path] = true
			}
		} else {
			rep.Dangling = append(rep.Dangling, fmt.Sprintf("[[%s]] in %s", raw, src))
		}
	}
	for _, n := range active {
		for _, raw := range links.Wikilinks(n.Body) {
			check(raw, shortID(n.ID)+" "+n.Rel)
		}
	}
	if d != nil {
		for _, t := range d.Tasks() {
			for _, raw := range links.Wikilinks(t.Text) {
				check(raw, "task "+shortID(t.ID()))
			}
		}
	}
	for _, n := range active {
		if n.Reserved() {
			continue // machine task-detail notes aren't held to KB hygiene
		}
		rep.NoteCount++
		handle := shortID(n.ID) + " " + n.Rel
		if !hasExplicitDescription(n) {
			rep.MissingDesc = append(rep.MissingDesc, handle)
		}
		if !linked[n.Path] {
			rep.Orphans = append(rep.Orphans, handle)
		}
	}
	return rep
}

// hasExplicitDescription reports whether a note carries a `description:` line in
// its frontmatter (kept in Extra, since nt doesn't model the key).
func hasExplicitDescription(n *note.Note) bool {
	for _, line := range n.Extra {
		if k, _, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "description") {
			return true
		}
	}
	return false
}

// printNoteHygiene emits the informational (non-failing) note-quality summary,
// naming a few offenders so they're actionable (not just a count).
func printNoteHygiene(nl noteLint) {
	if len(nl.MissingDesc) == 0 && len(nl.Orphans) == 0 {
		return
	}
	fmt.Printf("note hygiene: %d note(s)", nl.NoteCount)
	if len(nl.MissingDesc) > 0 {
		fmt.Printf(", %d without a description:", len(nl.MissingDesc))
	}
	if len(nl.Orphans) > 0 {
		fmt.Printf(", %d orphan(s)", len(nl.Orphans))
	}
	fmt.Println()
	if len(nl.MissingDesc) > 0 {
		fmt.Printf("  no description (add one so `nt index` is scannable): %s\n", sampleList(nl.MissingDesc, 8))
	}
	if len(nl.Orphans) > 0 {
		fmt.Printf("  orphans (nothing links to them): %s\n", sampleList(nl.Orphans, 8))
	}
}

// sampleList joins up to n items, appending "(+K more)" when it truncates.
func sampleList(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" (+%d more)", len(items)-n)
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
	// With --help, or when stdin is a terminal (no piped event — almost
	// certainly a human or agent exploring what it does rather than the hook
	// firing), print the contract + settings snippet instead of silently
	// blocking on stdin or exiting with no output at all.
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printHookHelp()
			return 0
		}
	}
	if isCharDevice(os.Stdin) {
		printHookHelp()
		return 0
	}
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

func printHookHelp() {
	fmt.Print(`nt hook — mirror a Claude Code TodoWrite list into your nt store.

It is a PostToolUse hook, not an interactive command: it reads the hook's JSON
event from stdin, upserts each todo as a task (tagged src:claude, idempotent),
is silent, and always exits 0. Wire it once into Claude Code's settings
(~/.claude/settings.json or a project .claude/settings.json):

  {
    "hooks": {
      "PostToolUse": [
        {
          "matcher": "TodoWrite",
          "hooks": [ { "type": "command", "command": "nt hook" } ]
        }
      ]
    }
  }

Then your agent's todo list is captured automatically as you work. Full setup
and the status mapping: docs/claude-integration.md. For typed agent tools
(nt_add, nt_index, nt_search, …) instead of the hook, see: nt mcp install.
`)
}
