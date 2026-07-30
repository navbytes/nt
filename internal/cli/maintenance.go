package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/aisync"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/undo"
	"github.com/navbytes/nt/internal/workstream"
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
	// Resolve EVERY handle before writing anything — the same guarantee cmdRm
	// documents and enforces. The previous loop saved as it went and returned
	// on the first unresolvable handle, so `nt archive good1 good2 typo` left
	// good1 and good2 archived (dropped out of index/search/recall) while the
	// only output was an error and a non-zero exit. Nobody re-checks a command
	// that reported failure, so that silently shrank the store.
	resolved := make([]*note.Note, 0, len(handles))
	for _, h := range handles {
		n, err := resolveNote(notes, h)
		if err != nil {
			return fail(fmt.Errorf("archive: %w", err))
		}
		resolved = append(resolved, n)
	}
	count := 0
	for _, n := range resolved {
		n.Archived = !unarchive
		n.Updated = time.Now().Format(time.RFC3339)
		// A mid-loop Save failure can still leave a partial application, but
		// that needs an I/O error rather than a typo — and every note that did
		// flip is reported by the count below.
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

func cmdUndo(args []string) int { return runReversal(args, false) }
func cmdRedo(args []string) int { return runReversal(args, true) }

// runReversal implements `nt undo` and `nt redo`. Two independent single-level
// journals exist — tasks (undo.jsonl, via mutate.Engine) and notes
// (notes-undo.jsonl, via the note package) — because they revert completely
// differently (a per-line inverse validated against a Doc, vs. a whole-file
// byte restore). "The last thing I did" can be either, so this peeks both and
// acts on whichever is more recent, rather than a task-only view that would
// silently ignore a newer note edit (or vice versa).
//
// On a shared multi-agent store the journal interleaves every writer's
// changes, so the last change is often NOT yours: when NT_WORKSTREAM is set,
// reverting another workstream's change is refused unless --force. Either way
// what was touched is printed, so a reversal is never silent about it.
func runReversal(args []string, isRedo bool) int {
	verb, past := "undo", "undid"
	if isRedo {
		verb, past = "redo", "redid"
	}
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	force := fs.Bool("force", false, "revert even if the last change belongs to another workstream")
	ws := fs.String("workstream", "", "act as this workstream (default: NT_WORKSTREAM)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	cur := workstream.Scope(*ws)

	taskTxn, _, taskPending := e.PeekUndoTxn()
	_, taskIsRedo, _ := e.PeekUndo()
	taskEligible := taskPending && taskIsRedo == isRedo

	noteEntry, notePending, nerr := note.PeekUndo(e.S)
	if nerr != nil {
		return fail(fmt.Errorf("%s: %w", verb, nerr))
	}
	noteEligible := notePending && note.IsRedoEntry(noteEntry.Op) == isRedo

	if !taskEligible && !noteEligible {
		fmt.Printf("nothing to %s\n", verb)
		return 0
	}
	useTask := taskEligible
	if taskEligible && noteEligible {
		taskTS, _ := time.Parse(time.RFC3339Nano, taskTxn.TS)
		noteTS, _ := time.Parse(time.RFC3339Nano, noteEntry.TS)
		useTask = !noteTS.After(taskTS) // note strictly newer -> revert the note instead
	}

	if useTask {
		var txn undo.Txn
		var did bool
		var err error
		if isRedo {
			txn, did, err = e.Redo(cur, *force)
		} else {
			txn, did, err = e.UndoScoped(cur, *force)
		}
		if err != nil {
			return fail(err)
		}
		if !did {
			fmt.Printf("nothing to %s\n", verb)
			return 0
		}
		op := strings.TrimPrefix(txn.Op, "redo:")
		fmt.Printf("%s: %s (%d task(s) affected)\n", past, op, len(txn.Changes))
		for _, c := range txn.Changes {
			// After the reversal each task's live line is the change's Before image
			// (redo swaps images when journaling, so Before is always "what it is now").
			line, verbed := c.Before, "restored"
			if line == "" {
				line, verbed = c.After, "removed"
			}
			if t, ok := task.ParseLine(line); ok {
				fmt.Printf("  %s %s  %s\n", verbed, shortID(c.ID), t.Text)
			}
		}
		return 0
	}

	path, op, did, err := note.ApplyReversal(e.S, cur, *force, isRedo)
	if err != nil {
		return fail(fmt.Errorf("%s: %w", verb, err))
	}
	if !did {
		fmt.Printf("nothing to %s\n", verb)
		return 0
	}
	rel := path
	if r, rerr := filepath.Rel(e.S.NotesDir(), path); rerr == nil {
		rel = r
	}
	fmt.Printf("%s: %s note %s\n", past, op, rel)
	return 0
}

func cmdEdit(args []string) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	appendTxt := fs.String("append", "", "append markdown to the note body without an editor (agent-safe)")
	appendFile := fs.String("append-file", "", "append the contents of a file ('-' = stdin) to the note body")
	body := fs.String("body", "", "replace the whole note body with this literal text — no temp file needed; use --body-file for long/multi-line content")
	bodyFile := fs.String("body-file", "", "replace the note body from a file ('-' = stdin); immune to shell quoting")
	oldString := fs.String("old-string", "", "exact existing text in the body to replace — must match exactly once; pair with --new-string for a targeted fix without resending the whole body")
	newString := fs.String("new-string", "", "replacement text for --old-string (empty deletes the matched text)")
	title := fs.String("title", "", "set the note's title (frontmatter + body H1) without an editor")
	desc := fs.String("desc", "", "set the note's one-line description (frontmatter) without an editor")
	fs.StringVar(desc, "description", "", "alias for --desc")
	descFile := fs.String("desc-file", "", "read the description from a file ('-' = stdin); immune to shell quoting, same as --body-file")
	validFrom := fs.String("valid-from", "", "set: this fact is only true from this date/time on (YYYY-MM-DD or RFC3339)")
	validUntil := fs.String("valid-until", "", "set: this fact stops being true after this date/time (YYYY-MM-DD or RFC3339) — nt_recall down-ranks and flags it 'expired' past this")
	clearValidFrom := fs.Bool("clear-valid-from", false, "remove the valid_from constraint")
	clearValidUntil := fs.Bool("clear-valid-until", false, "remove the valid_until constraint")
	halfLife := fs.String("half-life", "", "set the relevance half-life (Nd/Nw/Nm/Ny, or 'none') — the note fades in recall/index as it ages un-reconfirmed; `nt touch` resets the clock")
	reviewed := fs.String("reviewed", "", "set the last-reconfirmed date (YYYY-MM-DD or RFC3339) — the decay clock's reset point (prefer `nt touch` for today)")
	clearHalfLife := fs.Bool("clear-half-life", false, "remove the half_life (stop decaying)")
	clearReviewed := fs.Bool("clear-reviewed", false, "remove the reviewed date")
	// Simulation finding: agents pass --source reflexively (every WRITE command
	// takes it) and got an opaque "flag provided but not defined". Define it so
	// we can explain instead: provenance is set at creation and edits keep it.
	sourceFlag := fs.String("source", "", "not applicable on edit — provenance is recorded at creation (nt add/note) and edits keep the original source")
	expectMtime := fs.String("expect-mtime", "", "optional: the mtime from a prior `nt show --json` of this note — refuse instead of overwriting if it changed on disk since (best-effort; omit if you don't have one)")
	flags, positional := splitArgs(args, map[string]bool{"clear-valid-from": true, "clear-valid-until": true, "clear-half-life": true, "clear-reviewed": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("edit: need an id (or note:slug)"))
	}
	if strings.TrimSpace(*sourceFlag) != "" {
		return usageErr(fmt.Errorf("edit: --source doesn't apply here — provenance is recorded at creation (nt add/note --source) and edits keep the original; drop the flag and rerun"))
	}
	handle := positional[0]
	appendVal, aerr := resolveBody(*appendTxt, *appendFile)
	if aerr != nil {
		return usageErr(fmt.Errorf("edit: %w", aerr))
	}
	bodyVal, berr := resolveBody(*body, *bodyFile)
	if berr != nil {
		return usageErr(fmt.Errorf("edit: %w", berr))
	}
	// Same shell-quoting escape hatch --body-file gives the body: backticks in a
	// --desc value are expanded before nt sees them, silently truncating it.
	descVal, derr := resolveBody(*desc, *descFile)
	if derr != nil {
		return usageErr(fmt.Errorf("edit: %s", strings.ReplaceAll(derr.Error(), "--body", "--desc")))
	}
	// --old-string/--new-string only make sense as a pair: one alone has no
	// target (new-string) or nothing to put in its place (old-string), and
	// --new-string legitimately being "" (a deletion) means we can't use
	// emptiness to infer whether it was passed — check what was actually set.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	replacing := set["old-string"] || set["new-string"]
	if set["old-string"] != set["new-string"] {
		return usageErr(fmt.Errorf("edit: --old-string and --new-string must be given together"))
	}
	if replacing && strings.TrimSpace(*oldString) == "" {
		return usageErr(fmt.Errorf("edit: --old-string cannot be empty — there's no text to search for"))
	}
	if strings.TrimSpace(*validFrom) != "" && *clearValidFrom {
		return usageErr(fmt.Errorf("edit: --valid-from and --clear-valid-from are mutually exclusive"))
	}
	if strings.TrimSpace(*validUntil) != "" && *clearValidUntil {
		return usageErr(fmt.Errorf("edit: --valid-until and --clear-valid-until are mutually exclusive"))
	}
	editModes := 0
	for _, on := range []bool{appendVal != "", bodyVal != "", replacing} {
		if on {
			editModes++
		}
	}
	if editModes > 1 {
		return usageErr(fmt.Errorf("edit: --append, --body/--body-file, and --old-string/--new-string are mutually exclusive — pick one way to change the body per call"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	// Notes are single files — edit in place (safe, atomic save). Accept an
	// explicit note: prefix or any bare note handle (slug/title/short id), the same
	// handle every other note verb takes.
	notes, _ := note.List(e.S)
	// Non-interactive edits (--append / --body / --body-file / --old-string+
	// --new-string / --desc): the agent path. A mangled or growing note used to
	// be fixable only via $EDITOR or a whole-note supersede (which churns the id
	// and every inbound link); this edits in place.
	validitySet := strings.TrimSpace(*validFrom) != "" || strings.TrimSpace(*validUntil) != "" || *clearValidFrom || *clearValidUntil
	decaySet := strings.TrimSpace(*halfLife) != "" || strings.TrimSpace(*reviewed) != "" || *clearHalfLife || *clearReviewed
	if appendVal != "" || bodyVal != "" || replacing || strings.TrimSpace(descVal) != "" || strings.TrimSpace(*title) != "" || validitySet || decaySet {
		n, nerr := resolveNote(notes, strings.TrimPrefix(handle, "note:"))
		if nerr != nil {
			return fail(fmt.Errorf("edit: %w (non-interactive edits apply to notes; for tasks use `nt update`)", nerr))
		}
		// Raw bytes before the edit, for note.RecordUndo below — best-effort: if
		// this read fails, the edit still proceeds, just without an undo entry.
		beforeRaw, _ := os.ReadFile(n.Path)
		verb := ""
		switch {
		case appendVal != "":
			b := strings.TrimRight(n.Body, "\n")
			if b != "" {
				b += "\n\n"
			}
			n.Body = b + strings.TrimSpace(appendVal) + "\n"
			verb = "appended to"
		case bodyVal != "":
			n.Body = bodyVal
			verb = "replaced body of"
		case replacing:
			count := strings.Count(n.Body, *oldString)
			switch count {
			case 0:
				// Simulation finding: agents target the description text with
				// --old-string and get a bare "not found" — say WHERE the text
				// actually lives instead of leaving them to guess.
				if strings.Contains(n.Description(1<<20), *oldString) {
					return fail(fmt.Errorf("edit: --old-string matches the DESCRIPTION, not the body — the description is a separate field; replace it with `nt edit %s --desc \"…\"`", shortID(n.ID)))
				}
				return fail(fmt.Errorf("edit: --old-string not found in %s's body — run `nt show %s` to see the current text (descriptions are a separate field: --desc)", shortID(n.ID), shortID(n.ID)))
			case 1:
				n.Body = strings.Replace(n.Body, *oldString, *newString, 1)
				verb = "edited"
			default:
				return fail(fmt.Errorf("edit: --old-string matches %d times in %s's body — make it longer/more specific so the match is unambiguous", count, shortID(n.ID)))
			}
		}
		// --title was the missing repair path. Without it a wrong title was
		// permanent from the CLI: `nt mv` renames the file but pins the OLD
		// title into frontmatter, where it then beats the body H1, so editing
		// the H1 changed nothing visible. Rewrite the H1 too when it still
		// carries the previous title, so frontmatter, heading and filename
		// don't drift apart.
		if tt := strings.TrimSpace(*title); tt != "" {
			if old := strings.TrimSpace(n.Title); old != "" {
				n.Body = replaceLeadingHeading(n.Body, old, tt)
			}
			n.Title = tt
			if verb == "" {
				verb = "retitled"
			}
		}
		if d := strings.TrimSpace(descVal); d != "" {
			setNoteDescription(n, d)
			if verb == "" {
				verb = "set description of"
			}
		}
		if vf := strings.TrimSpace(*validFrom); vf != "" {
			n.ValidFrom = vf
			if verb == "" {
				verb = "set validity of"
			}
		} else if *clearValidFrom {
			n.ValidFrom = ""
			if verb == "" {
				verb = "set validity of"
			}
		}
		if vu := strings.TrimSpace(*validUntil); vu != "" {
			n.ValidUntil = vu
			if verb == "" {
				verb = "set validity of"
			}
		} else if *clearValidUntil {
			n.ValidUntil = ""
			if verb == "" {
				verb = "set validity of"
			}
		}
		if hl := strings.TrimSpace(*halfLife); hl != "" {
			if _, ok, isNone := note.ParseHalfLife(hl); !ok && !isNone {
				return usageErr(fmt.Errorf("edit: --half-life must be Nd/Nw/Nm/Ny or 'none', got %q", hl))
			}
			n.HalfLife = hl
			if verb == "" {
				verb = "set decay of"
			}
		} else if *clearHalfLife {
			n.HalfLife = ""
			if verb == "" {
				verb = "set decay of"
			}
		}
		if rv := strings.TrimSpace(*reviewed); rv != "" {
			if _, ok := note.ParseFlexDate(rv); !ok {
				return usageErr(fmt.Errorf("edit: --reviewed must be YYYY-MM-DD or RFC3339, got %q", rv))
			}
			n.Reviewed = rv
			if verb == "" {
				verb = "set decay of"
			}
		} else if *clearReviewed {
			n.Reviewed = ""
			if verb == "" {
				verb = "set decay of"
			}
		}
		n.Updated = time.Now().Format(time.RFC3339)
		if err := n.SaveIfUnchanged(strings.TrimSpace(*expectMtime)); err != nil {
			var stale *note.StaleNoteError
			if errors.As(err, &stale) {
				return fail(fmt.Errorf("edit: %s changed on disk since you loaded it — `nt show %s --json` for the current text and mtime, then retry", shortID(n.ID), shortID(n.ID)))
			}
			return fail(err)
		}
		// Best-effort: the edit is now undoable (`nt undo`), but a journal
		// failure must never fail the edit itself — the write already succeeded.
		if beforeRaw != nil {
			_ = note.RecordUndo(e.S, note.UndoEntry{
				Op: verb, TS: time.Now().UTC().Format(time.RFC3339Nano), WS: workstream.Env(),
				Path: n.Path, Before: string(beforeRaw),
			})
		}
		warnDanglingLinks(e, n)
		fmt.Printf("%s %s  %s\n", verb, shortID(n.ID), n.Rel)
		return 0
	}
	// Everything below hands the terminal to $EDITOR. In a non-interactive
	// context (a pipe, CI, an agent shelling out) that spews raw escape
	// sequences and hangs — fail with the agent-safe alternatives instead.
	// (Resolution still runs first, so a bad handle errors as a bad handle.)
	editNote := func(n *note.Note) int {
		if code := requireEditorTerminal(); code != 0 {
			return code
		}
		return runEditor(n.Path)
	}
	if strings.HasPrefix(handle, "note:") {
		want := strings.TrimPrefix(handle, "note:")
		if n, nerr := resolveNote(notes, want); nerr == nil {
			return editNote(n)
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
			return editNote(n)
		}
		return fail(fmt.Errorf("edit: no task or note %q", handle))
	}
	if code := requireEditorTerminal(); code != 0 {
		return code
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
	integrations := fs.Bool("integrations", false, "check Claude Code/OpenCode/Pi integration wiring instead of the task/note store (read-only, never fixes)")
	flags, _ := splitArgs(args, map[string]bool{"check": true, "integrations": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if *integrations {
		return runIntegrationsDoctor()
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
	// Task-side duplicate lint (informational): identical bare captures slip past
	// the write-time warning when the writers never see each other's stderr.
	dupTasks := lintTaskDups(e)
	taskProblem := rep.HasProblems()
	noteProblem := len(nl.Dangling) > 0 || len(nl.DupKeys) > 0

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
	for _, dk := range nl.DupKeys {
		fmt.Println("  ⚠ duplicate frontmatter key " + dk)
	}

	// Reclaimable dead weight (superseded stubs, stranded task details) — doctor
	// is the curation entry point, so it points at the mechanized cleanup.
	gcCount := len(gcCandidates(e, time.Now().AddDate(0, 0, -30).Format("2006-01-02")))

	if !taskProblem && !noteProblem {
		if nl.hasHygieneNotices() || gcCount > 0 || len(dupTasks) > 0 {
			fmt.Println("tasks and links are healthy — hygiene notices below")
		} else {
			fmt.Println("store is healthy — no issues found")
		}
		printNoteHygiene(nl)
		printTaskDups(dupTasks)
		if gcCount > 0 {
			fmt.Printf("  %d reclaimable note(s) (superseded/stranded >30d) — `nt gc` to review, `nt gc --yes` to trash\n", gcCount)
		}
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
	if len(nl.Dangling) > 0 {
		fmt.Printf("%d dangling note link(s) — fix the [[target]] or the note it points to\n", len(nl.Dangling))
	}
	if len(nl.DupKeys) > 0 {
		fmt.Printf("%d note(s) with a duplicate frontmatter key — only the first occurrence was used; hand-fix the file\n", len(nl.DupKeys))
	}
	printNoteHygiene(nl)
	printTaskDups(dupTasks)
	if *check {
		return 1
	}
	return 0
}

// lintTaskDups finds pairs of OPEN tasks whose titles overlap heavily (the same
// dupTitleOverlap threshold the write-time warning uses) — likely duplicate
// captures from writers who never saw each other's stderr hint. Informational,
// never a doctor failure; capped at 5 pairs so a messy store stays readable.
func lintTaskDups(e *mutate.Engine) []string {
	d, err := e.Read()
	if err != nil || d == nil {
		return nil
	}
	var open []*task.Task
	for _, t := range d.Tasks() {
		if !t.Done {
			open = append(open, t)
		}
	}
	var out []string
	for i := 0; i < len(open); i++ {
		for j := i + 1; j < len(open); j++ {
			if note.TitleOverlap(open[i].Display(), open[j].Display()) >= dupTitleOverlap {
				out = append(out, fmt.Sprintf("%s ≈ %s (%s)", shortID(open[i].ID()), shortID(open[j].ID()), open[i].Display()))
				if len(out) >= 5 {
					return out
				}
			}
		}
	}
	return out
}

// printTaskDups emits the informational duplicate-open-task notice (one line).
func printTaskDups(pairs []string) {
	if len(pairs) == 0 {
		return
	}
	fmt.Printf("  possible duplicate open tasks: %s — review with `nt show <id>`, then link or `nt rm` one\n", strings.Join(pairs, ", "))
}

// noteLint is the KB-side health report `nt doctor` produces alongside the task
// reconciliation.
type noteLint struct {
	Dangling []string // "[[target]] in <source>" — an unresolved wiki-link (a real break)
	// DupKeys is "<handle>: <key>" per note carrying a duplicate plural
	// frontmatter key (tags:/aliases:) — Load already dropped the duplicate
	// occurrence in favor of the first (see note.Note.DupKeys), but the file
	// on disk is tampered or corrupt either way, and a hand-edited/externally-
	// sourced note is exactly the case nt's own write-side guard can't cover.
	DupKeys      []string
	NoteCount    int
	MissingDesc  []string // handles of active notes with no explicit `description:`
	Orphans      []string // handles of active notes nothing links to (informational)
	NearDups     []string // "a ≈ b" pairs of active notes with near-duplicate titles
	PinnedCount  int      // notes in the always-shown index tier (rules/memory/ref/pin)
	OldestPinned []string // "handle (aged Nd)" — staleness candidates when the tier is oversized
	BadDecay     []string // "handle: problem" — unparseable half_life / future or bad reviewed (warn-and-preserve; a bad value never affects ranking)
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

	// Duplicate-key scan runs over ALL notes, not just active/non-reserved —
	// unlike the hygiene checks below, a tampered frontmatter key matters on
	// an archived or task-detail note too, not just ones in the working set.
	for _, n := range allNotes {
		if len(n.DupKeys) > 0 {
			rep.DupKeys = append(rep.DupKeys, shortID(n.ID)+" "+n.Rel+": "+strings.Join(n.DupKeys, ", "))
		}
	}

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
	var pinnedNotes []*note.Note
	for _, n := range active {
		if n.Reserved() {
			continue // machine task-detail notes aren't held to KB hygiene
		}
		rep.NoteCount++
		if n.Pinned() {
			rep.PinnedCount++
			pinnedNotes = append(pinnedNotes, n)
		}
		handle := shortID(n.ID) + " " + n.Rel
		// Decay hygiene (spec §3): a malformed half_life/reviewed silently means
		// "no decay" everywhere else (a bad value must never zero a note), so
		// doctor is the one place it's made visible.
		if hl := strings.TrimSpace(n.HalfLife); hl != "" {
			if _, okHL, isNone := note.ParseHalfLife(hl); !okHL && !isNone {
				rep.BadDecay = append(rep.BadDecay, handle+": half_life "+strconv.Quote(hl)+" is not Nd/Nw/Nm/Ny or \"none\" — decay is OFF for this note")
			}
		}
		if rv := strings.TrimSpace(n.Reviewed); rv != "" {
			if t, okRv := note.ParseFlexDate(rv); !okRv {
				rep.BadDecay = append(rep.BadDecay, handle+": reviewed "+strconv.Quote(rv)+" is not YYYY-MM-DD or RFC3339")
			} else if t.After(time.Now().AddDate(0, 0, 1)) {
				rep.BadDecay = append(rep.BadDecay, handle+": reviewed "+rv+" is in the future — the decay clock never advances")
			}
		}
		if !hasExplicitDescription(n) {
			rep.MissingDesc = append(rep.MissingDesc, handle)
		}
		// Standalone kinds — lessons, decisions, reference notes — are consulted
		// through recall/index, not through inbound links; calling them orphans
		// buried the real strays in noise (field-study: every orphan report was
		// a false alarm).
		if !linked[n.Path] && !standaloneKind(n) {
			rep.Orphans = append(rep.Orphans, handle)
		}
	}
	// Near-duplicate titles are the store rot that degrades recall most —
	// surface them here since doctor is the curation entry point (nt distill
	// exposes the same pairs uncapped, with full stub fields, for an agent to
	// review). One pass, shared logic — see note.NearDupPairs.
	for _, p := range note.NearDupPairs(active) {
		rep.NearDups = append(rep.NearDups, fmt.Sprintf("%s ≈ %s", shortID(p.A.ID)+" "+p.A.Rel, shortID(p.B.ID)+" "+p.B.Rel))
	}
	// When the pinned tier is oversized, name the oldest members with their age
	// — "demote stale notes" is only actionable if nt says WHICH are stale.
	if rep.PinnedCount > note.TierPinnedWarn {
		sort.SliceStable(pinnedNotes, func(i, j int) bool { return pinnedNotes[i].ChangedDate() < pinnedNotes[j].ChangedDate() })
		today := time.Now()
		for i, n := range pinnedNotes {
			if i >= 5 {
				break
			}
			age := ""
			if d, err := time.Parse("2006-01-02", n.ChangedDate()); err == nil {
				age = fmt.Sprintf(" (aged %dd)", int(today.Sub(d).Hours()/24))
			}
			rep.OldestPinned = append(rep.OldestPinned, shortID(n.ID)+" "+n.Rel+age)
		}
	}
	return rep
}

// standaloneKind reports whether a note's class makes it legitimately
// inbound-linkless: lessons and rules (consumed via recall/export), decision
// and reference notes (consumed via index/search), and daily journal entries.
func standaloneKind(n *note.Note) bool {
	for _, t := range n.Tags {
		switch t {
		case "lesson", "rule", "memory-core", "decision", "ref", "reference":
			return true
		}
	}
	for _, f := range []string{"lessons/", "decisions/", "ref/", "rules/", "journal/", "memory/"} {
		if strings.HasPrefix(n.Rel, f) {
			return true
		}
	}
	return false
}

// setNoteDescription sets or replaces the `description:` frontmatter line (kept
// in Extra, since nt doesn't model the key).
func setNoteDescription(n *note.Note, d string) {
	for i, line := range n.Extra {
		if k, _, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "description") {
			n.Extra[i] = "description: " + d
			return
		}
	}
	n.Extra = append(n.Extra, "description: "+d)
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
	if len(nl.MissingDesc) == 0 && len(nl.Orphans) == 0 && len(nl.NearDups) == 0 {
		return
	}
	fmt.Printf("note hygiene: %d note(s)", nl.NoteCount)
	if len(nl.MissingDesc) > 0 {
		fmt.Printf(", %d without a description", len(nl.MissingDesc))
	}
	if len(nl.NearDups) > 0 {
		fmt.Printf(", %d near-duplicate title pair(s)", len(nl.NearDups))
	}
	fmt.Println()
	if len(nl.MissingDesc) > 0 {
		fmt.Printf("  no description (add one so `nt index` is scannable): %s\n", sampleList(nl.MissingDesc, 8))
	}
	// Orphans are one line, no sample list: field data showed most active notes
	// are legitimately unlinked (fresh captures, standalone references), and an
	// 8-item name dump trained readers to ignore doctor entirely.
	if len(nl.Orphans) > 0 {
		fmt.Printf("  %d unlinked note(s) (normal for fresh captures) — nt links --orphans lists them\n", len(nl.Orphans))
	}
	if len(nl.NearDups) > 0 {
		fmt.Printf("  near-duplicates (consolidate with `nt supersede <old> --by <new>`): %s (tag one 'distinct' to acknowledge a deliberate fork)\n", sampleList(nl.NearDups, 6))
	}
	// "Always shown" invites dumping — make the pinned tier's cost legible once
	// it outgrows what a session-start load should pay for. ~25 tokens per stub
	// row (measured on rendered output, not guessed).
	if nl.PinnedCount > note.TierPinnedWarn {
		fmt.Printf("  pinned tier is %d notes (≈%d tokens shown at EVERY session start) — demote stale rules/memory/ref notes (nt archive) or consolidate\n",
			nl.PinnedCount, nl.PinnedCount*25)
		if len(nl.OldestPinned) > 0 {
			fmt.Printf("  oldest pinned (staleness candidates): %s\n", sampleList(nl.OldestPinned, 5))
		}
	}
	if len(nl.BadDecay) > 0 {
		fmt.Printf("  decay frontmatter problems (value preserved, decay inert until fixed):\n")
		for _, b := range nl.BadDecay {
			fmt.Printf("    %s\n", b)
		}
	}
}

// hasHygieneNotices reports whether the informational note-quality summary has
// anything to say — used to keep the headline honest.
func (nl noteLint) hasHygieneNotices() bool {
	return len(nl.MissingDesc) > 0 || len(nl.Orphans) > 0 || len(nl.NearDups) > 0 || nl.PinnedCount > note.TierPinnedWarn || len(nl.BadDecay) > 0
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
		"notes-undo.jsonl\n" +
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

// cmdSync is the thin wrapper the git-native shared-team-memory pattern needs:
// commit local edits, pull, reconcile the union-merge driver's duplicate-ULID
// leftovers with `doctor`, commit the reconciliation, push. Every step reuses
// plain git — `nt sync` doesn't invent a protocol, it just sequences the
// commands a team would otherwise have to remember to run in this order after
// `nt git-init` (SPEC §6.4). A merge CONFLICT (two people editing the same
// note — tasks.txt/done.txt auto-merge via .gitattributes, but individual
// note files don't) stops the sequence with git's own conflict markers left
// in place; resolve by hand and re-run.
func cmdSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	noPush := fs.Bool("no-push", false, "pull + reconcile only, don't push")
	message := fs.String("message", "nt sync", "commit message for the local-edits commit")
	flags, _ := splitArgs(args, map[string]bool{"no-push": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	dir := e.S.Dir
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return fail(fmt.Errorf("sync: %s isn't a git repo yet — run `nt git-init` first", dir))
	}
	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// Commit local edits FIRST: pulling with a dirty tree either refuses outright
	// or mixes uncommitted changes into the merge — committing first keeps this
	// sync's edits as their own commit, mergeable like anyone else's.
	if status, serr := run("git", "status", "--porcelain"); serr != nil {
		return fail(fmt.Errorf("sync: git status: %s", status))
	} else if status != "" {
		if out, aerr := run("git", "add", "-A"); aerr != nil {
			return fail(fmt.Errorf("sync: git add: %s", out))
		}
		if out, cerr := run("git", "commit", "-m", *message); cerr != nil {
			return fail(fmt.Errorf("sync: git commit: %s", out))
		}
	}

	// --no-rebase pins the strategy to an ordinary merge regardless of the
	// caller's global git config (pull.rebase unset raises "divergent
	// branches" instead of pulling) — a merge is also the right choice here:
	// rebase would rewrite commit hashes another team member may have already
	// pushed on top of.
	if out, perr := run("git", "pull", "--no-edit", "--no-rebase"); perr != nil {
		return fail(fmt.Errorf("sync: git pull failed — resolve the conflict (likely a note under notes/, since only tasks.txt/done.txt auto-merge), `git add`+`git commit`, then re-run `nt sync`:\n%s", out))
	}

	// The union-merge driver keeps BOTH sides' lines verbatim when concurrent
	// branches added different tasks — doctor's existing duplicate-ULID
	// reconciliation is exactly the fix, and always safe to run: a no-op when
	// the pull introduced nothing to dedup.
	rep, derr := e.Doctor(true)
	if derr != nil {
		return fail(fmt.Errorf("sync: doctor: %w", derr))
	}
	if rep.Issues() > 0 {
		if out, aerr := run("git", "add", "-A"); aerr != nil {
			return fail(fmt.Errorf("sync: git add (post-doctor): %s", out))
		}
		if out, cerr := run("git", "commit", "-m", "nt doctor: reconcile merge duplicates"); cerr != nil {
			return fail(fmt.Errorf("sync: git commit (post-doctor): %s", out))
		}
	}

	if *noPush {
		fmt.Println("synced (pull + reconcile) — not pushed (--no-push)")
		return 0
	}
	if out, perr := run("git", "push"); perr != nil {
		return fail(fmt.Errorf("sync: git push: %s", out))
	}
	fmt.Println("synced: pulled, reconciled, pushed")
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

	// Error-triggered lesson recall (PostToolUse, matcher "Bash") — the same
	// loop the OpenCode/Pi integrations already run: a failed bash command
	// summons any recorded lesson that might explain it, instead of relying on
	// the agent remembering to ask. Exit 2 + stderr is Claude Code's hook
	// contract for feeding blocking feedback back to the model; every other
	// path (including a non-Bash event, or nothing relevant on record) exits 0
	// silently, matching the TodoWrite mirror above.
	if msg, fire := hookBashErrorRecall(note.Active(mustNotes(e)), data); fire {
		fmt.Fprintln(os.Stderr, msg)
		return 2
	}
	return 0
}

func printHookHelp() {
	fmt.Print(`nt hook — two Claude Code PostToolUse hooks in one command.

It is a PostToolUse hook, not an interactive command: it reads the hook's JSON
event from stdin and dispatches on tool_name.
  - matcher "TodoWrite": upserts each todo as a task (tagged src:claude,
    idempotent), silent, always exits 0.
  - matcher "Bash": on a FAILED command, searches recorded lessons for the
    command + error tail (the same loop the OpenCode/Pi integrations run) and,
    if it finds one, exits 2 with the lessons on stderr — Claude Code's
    block+reason contract for feeding it back to the model. Exits 0 silently
    otherwise (success, or nothing relevant on record).
Wire both into Claude Code's settings (~/.claude/settings.json or a project
.claude/settings.json):

  {
    "hooks": {
      "PostToolUse": [
        {
          "matcher": "TodoWrite",
          "hooks": [ { "type": "command", "command": "nt hook" } ]
        },
        {
          "matcher": "Bash",
          "hooks": [ { "type": "command", "command": "nt hook" } ]
        }
      ]
    }
  }

Then your agent's todo list is captured automatically as you work, and a
failed command that matches a recorded lesson surfaces it on the next turn.
Full setup: docs/claude-integration.md. For typed agent tools (nt_add,
nt_index, nt_search, …) instead of the hook, see: nt mcp install.
`)
}

// replaceLeadingHeading rewrites a body's opening "# old" heading to "# new"
// when it still carries the previous title, so `nt edit --title` doesn't leave
// the H1 contradicting the frontmatter. Bodies whose H1 was already something
// else are left alone — that heading is the author's, not a mirror of the title.
func replaceLeadingHeading(body, old, new string) string {
	trimmed := strings.TrimLeft(body, "\n")
	lead := body[:len(body)-len(trimmed)]
	if !strings.HasPrefix(trimmed, "# ") {
		return body
	}
	line, rest, _ := strings.Cut(trimmed, "\n")
	if strings.TrimSpace(strings.TrimPrefix(line, "# ")) != strings.TrimSpace(old) {
		return body
	}
	out := lead + "# " + new
	if rest != "" {
		out += "\n" + rest
	}
	return out
}
