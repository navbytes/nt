package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/navbytes/nt/internal/aisync"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// Store-maintenance and housekeeping commands: archive, undo, edit, path,
// rename/move, doctor, git-init, and the Claude Code TodoWrite hook. Split out of
// commands.go to keep that file focused on the task/note verbs (E6).

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
	for _, w := range rep.Warnings {
		fmt.Println("  ⚠ " + w)
	}
	if !rep.HasProblems() {
		fmt.Println("store is healthy — no issues found")
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
	if *check {
		return 1
	}
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
