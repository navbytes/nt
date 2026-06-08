package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
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
	var last *note.Note
	count := 0
	for _, h := range handles {
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
	if count == 1 {
		fmt.Printf("tagged %s  @%s\n", shortID(last.ID), strings.Join(last.Tags, " @"))
	} else {
		fmt.Printf("tagged %d notes  (%s)\n", count, strings.Join(ops, " "))
	}
	return 0
}

// rmNote deletes a note by moving it to the store's .trash/ (recoverable),
// refusing if other notes link to it unless force is set.
func rmNote(e *mutate.Engine, n *note.Note, force bool) int {
	if back := links.Backlinks(e.S, n.ID, n.Rel); len(back) > 0 && !force {
		fmt.Fprintf(os.Stderr, "rm: %q has %d inbound link(s) that would dangle:\n", n.Rel, len(back))
		for _, h := range back {
			fmt.Fprintf(os.Stderr, "  %s\n", strings.TrimSpace(h.Text))
		}
		fmt.Fprintln(os.Stderr, "  re-run with --force to delete anyway")
		return 1
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
