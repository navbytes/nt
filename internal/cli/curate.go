package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// resolveNote resolves a handle (slug/title/path or the short id nt prints) to a
// single note, surfacing ambiguity.
func resolveNote(notes []*note.Note, handle string) (*note.Note, error) {
	it, ok := links.Resolve(handle, nil, notes)
	if !ok {
		if it.Kind == "ambiguous" {
			return nil, fmt.Errorf("%q is ambiguous (%s) — qualify with a folder", handle, it.Title)
		}
		return nil, fmt.Errorf("no note %q", handle)
	}
	if it.Kind != "note" {
		return nil, fmt.Errorf("%q is not a note", handle)
	}
	for _, n := range notes {
		if n.Path == it.Path {
			return n, nil
		}
	}
	return nil, fmt.Errorf("no note %q", handle)
}

// cmdShow prints a single note's content in the terminal — the basic "read it
// back" action a KB needs, without an $EDITOR or web round-trip.
func cmdShow(args []string) int {
	flags, positional := splitArgs(args, map[string]bool{"json": true})
	asJSON := false
	for _, f := range flags {
		if f == "--json" || f == "-json" {
			asJSON = true
		}
	}
	if len(positional) == 0 {
		return fail(fmt.Errorf("show: need a note handle (slug/title/id)"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	n, err := resolveNote(notes, strings.Join(positional, " "))
	if err != nil {
		return fail(fmt.Errorf("show: %w", err))
	}
	if asJSON {
		return printJSON(notesToJSON([]*note.Note{n})[0])
	}
	fmt.Printf("# %s\n", n.Title)
	meta := []string{shortID(n.ID), n.Rel}
	if len(n.Tags) > 0 {
		meta = append(meta, "@"+strings.Join(n.Tags, " @"))
	}
	fmt.Printf("%s\n\n", strings.Join(meta, "  ·  "))
	body := strings.TrimSpace(n.Body)
	// Drop a leading "# Title" H1 — it just echoes the header we already printed.
	if strings.HasPrefix(body, "# "+n.Title) {
		body = strings.TrimSpace(strings.TrimPrefix(body, "# "+n.Title))
	}
	fmt.Println(body)
	return 0
}

// cmdNotes lists notes (one row per note), optionally filtered by folder/tag —
// the note-side counterpart to `nt list` for tasks. Without it the only way to
// enumerate notes was `nt recall` (which leads with every task) or shelling to ls.
func cmdNotes(args []string) int {
	fs := flag.NewFlagSet("notes", flag.ContinueOnError)
	folder := fs.String("folder", "", "only notes under this folder")
	asJSON := fs.Bool("json", false, "machine-readable output")
	var tags stringSlice
	fs.Var(&tags, "tag", "only notes with this tag (repeatable, AND)")
	flags, _ := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	notes := note.Active(mustNotes(e))
	prefix := strings.Trim(*folder, "/")
	var out []*note.Note
	for _, n := range notes {
		if prefix != "" && !strings.HasPrefix(n.Rel, prefix+"/") {
			continue
		}
		match := true
		for _, want := range tags {
			if !contains(n.Tags, want) {
				match = false
				break
			}
		}
		if match {
			out = append(out, n)
		}
	}
	if *asJSON {
		return printJSON(notesToJSON(out))
	}
	if len(out) == 0 {
		fmt.Println("no notes")
		return 0
	}
	for _, n := range out {
		line := fmt.Sprintf("%s  %s", n.Rel, n.Title)
		if len(n.Tags) > 0 {
			line += "  @" + strings.Join(n.Tags, " @")
		}
		fmt.Println(line)
	}
	return 0
}

// cmdTags enumerates the tag vocabulary (notes + tasks) with counts — helps keep
// a controlled vocabulary clean.
func cmdTags(args []string) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	counts := map[string]int{}
	notes, _ := note.List(e.S)
	for _, n := range notes {
		for _, tg := range n.Tags {
			counts[tg]++
		}
	}
	if d, err := e.Read(); err == nil {
		for _, t := range d.Tasks() {
			for _, tg := range t.Tags() {
				counts[tg]++
			}
		}
	}
	if len(counts) == 0 {
		fmt.Println("no tags")
		return 0
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-20s %d\n", "@"+k, counts[k])
	}
	return 0
}

// cmdTag retags a note without a $EDITOR round-trip: nt tag <note> +add -remove …
// Frontmatter the agent didn't author (Obsidian properties) is preserved.
func cmdTag(args []string) int {
	// Split into note handles and +add/-remove ops, so one retag can apply to
	// many notes: `nt tag work/auth design/spec +reviewed -draft`.
	var handles, ops []string
	for _, a := range args {
		if strings.HasPrefix(a, "+") || strings.HasPrefix(a, "-") {
			ops = append(ops, a)
		} else {
			handles = append(handles, a)
		}
	}
	if len(handles) == 0 || len(ops) == 0 {
		return fail(fmt.Errorf("tag: usage: nt tag <note…> +add -remove …"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	d, _ := e.Read()

	// Partition handles into notes and tasks so `nt tag` retags either kind — the
	// skill presents tagging as a general curation verb, and a task id that isn't a
	// note shouldn't error with a confusing "no note".
	var noteHandles, taskHandles []string
	for _, h := range handles {
		if _, err := resolveNote(notes, h); err == nil {
			noteHandles = append(noteHandles, h)
		} else if d != nil {
			if _, terr := resolveHandle(d, h); terr == nil {
				taskHandles = append(taskHandles, h)
				continue
			}
			noteHandles = append(noteHandles, h) // keep as note so its error surfaces
		} else {
			noteHandles = append(noteHandles, h)
		}
	}

	var last *note.Note
	count := 0
	for _, h := range noteHandles {
		n, err := resolveNote(notes, h)
		if err != nil {
			return fail(fmt.Errorf("tag: %w", err))
		}
		for _, op := range ops {
			tg := strings.TrimPrefix(op[1:], "@")
			if strings.HasPrefix(op, "+") {
				if tg != "" && !contains(n.Tags, tg) {
					n.Tags = append(n.Tags, tg)
				}
			} else {
				n.Tags = removeStr(n.Tags, tg)
			}
		}
		n.Updated = time.Now().Format(time.RFC3339)
		if err := n.Save(); err != nil {
			return fail(err)
		}
		last, count = n, count+1
	}

	tcount := 0
	if len(taskHandles) > 0 {
		err := e.Apply("tag", func(d *task.Doc, rec *mutate.Recorder) error {
			for _, h := range taskHandles {
				t, err := resolveHandle(d, h)
				if err != nil {
					return fmt.Errorf("tag: %w", err)
				}
				rec.Before(t)
				for _, op := range ops {
					tg := strings.TrimPrefix(op[1:], "@")
					if strings.HasPrefix(op, "+") {
						t.AddTag(tg)
					} else {
						t.RemoveTag(tg)
					}
				}
				tcount++
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
	}

	switch {
	case count == 1 && tcount == 0:
		fmt.Printf("tagged %s  @%s\n", shortID(last.ID), strings.Join(last.Tags, " @"))
	case count+tcount == 0:
		fmt.Println("nothing tagged")
	default:
		fmt.Printf("tagged %d item(s)  (%s)\n", count+tcount, strings.Join(ops, " "))
	}
	return 0
}

// rmNote deletes a note by moving it to the store's .trash/ (recoverable). It
// guards against dangling links: a note with inbound [[links]] is not deleted
// unless the caller resolves them — strip the links first (unlink), delete anyway
// and leave them dangling (force), or, interactively, choose at the prompt.
func rmNote(e *mutate.Engine, n *note.Note, force, unlink, yes bool) int {
	back := links.Backlinks(e.S, n.ID, n.Rel)

	if len(back) > 0 && !force && !unlink {
		fmt.Fprintf(os.Stderr, "%q has %d inbound link(s) that would dangle:\n", n.Rel, len(back))
		for _, h := range back {
			fmt.Fprintf(os.Stderr, "  %s\n", strings.TrimSpace(h.Text))
		}
		if !interactive() {
			fmt.Fprintln(os.Stderr, "  --unlink to strip them first, or --force to delete anyway")
			return 1
		}
		switch prompt("Delete this note? [u]nlink & delete / [f]orce (leave dangling) / [c]ancel: ") {
		case "u", "unlink":
			unlink = true
		case "f", "force":
			// proceed: delete the note as-is, leaving the inbound links dangling
		default:
			fmt.Println("cancelled")
			return 0
		}
	} else if interactive() && !yes {
		// No inbound links (or already resolved via flags) — still confirm.
		if !confirm(fmt.Sprintf("Delete note %q?", n.Rel)) {
			fmt.Println("cancelled")
			return 0
		}
	}

	if unlink && len(back) > 0 {
		updated, err := e.UnlinkNote(n)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("unlinked %d reference(s)\n", updated)
	}

	trash := filepath.Join(e.S.Dir, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return fail(err)
	}
	dest := filepath.Join(trash, strings.ReplaceAll(n.Rel, "/", "_"))
	if err := os.Rename(n.Path, dest); err != nil {
		return fail(err)
	}
	fmt.Printf("deleted %s → .trash/\n", n.Rel)
	return 0
}

func removeStr(ss []string, want string) []string {
	var out []string
	for _, s := range ss {
		if s != want {
			out = append(out, s)
		}
	}
	return out
}
