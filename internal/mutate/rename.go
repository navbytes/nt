package mutate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// RenameNote renames or moves a note file and rewrites every [[link]] to it
// across tasks.txt and notes/ (preserving each link's folder prefix, #fragment,
// and |alias). dest is a new name or a folder/path relative to notes/ (.md
// optional); a bare name keeps the note in its current folder. It refuses a
// destination whose basename collides with another note (which would make bare
// links ambiguous). A pure move (same basename) needs no link rewrite, since
// resolution is by path-suffix. Like Archive it is not a single undo transaction.
func (e *Engine) RenameNote(src *note.Note, all []*note.Note, dest string) (newRel string, updated int, err error) {
	oldRel := src.Rel
	if oldRel == "" {
		if r, e2 := filepath.Rel(e.S.NotesDir(), src.Path); e2 == nil {
			oldRel = filepath.ToSlash(r)
		}
	}
	oldBase := base(oldRel)

	newRel = strings.TrimSpace(dest)
	if newRel == "" {
		return "", 0, fmt.Errorf("rename: empty destination")
	}
	if !strings.HasSuffix(newRel, ".md") {
		newRel += ".md"
	}
	newRel = filepath.ToSlash(newRel)
	if !strings.Contains(newRel, "/") { // bare name → keep the note's folder
		newRel = filepath.ToSlash(filepath.Join(filepath.Dir(oldRel), newRel))
	}
	// Slug each segment, the way `nt note` does when it derives a filename from
	// a title. Without this, `nt mv <note> "ref/My Long Name"` wrote a file with
	// literal spaces in it — the only file in the store nt hadn't slugged, and
	// inconsistent with every other path it creates.
	newRel = slugRel(newRel)
	newBase := base(newRel)
	if newBase == "" {
		return "", 0, fmt.Errorf("rename: invalid destination %q", dest)
	}

	// Refuse a basename that already belongs to another note (would make bare
	// links ambiguous). A pure folder move of the same name is fine.
	if !strings.EqualFold(oldBase, newBase) {
		for _, x := range all {
			if x.Path != src.Path && strings.EqualFold(base(x.Rel), newBase) {
				return "", 0, fmt.Errorf("rename: a note named %q already exists (%s)", newBase, x.Rel)
			}
		}
	}

	// Move the file. Defense in depth: the destination must stay within notes/ —
	// the web boundary already allowlists the folder, and newRel is built from a
	// trimmed dest, but assert containment so no caller (CLI included) can escape.
	newPath := filepath.Join(e.S.NotesDir(), filepath.FromSlash(newRel))
	if rel, e2 := filepath.Rel(e.S.NotesDir(), newPath); e2 != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", 0, fmt.Errorf("rename: destination escapes notes/: %q", dest)
	}
	if newPath != src.Path {
		if _, e2 := os.Stat(newPath); e2 == nil {
			return "", 0, fmt.Errorf("rename: %s already exists", newRel)
		}
		if e2 := os.MkdirAll(filepath.Dir(newPath), 0o755); e2 != nil {
			return "", 0, e2
		}
		if e2 := os.Rename(src.Path, newPath); e2 != nil {
			return "", 0, e2
		}
	}
	if strings.EqualFold(oldBase, newBase) {
		return newRel, 0, nil // pure move; suffix resolution needs no rewrite
	}

	// Rewrite task links in one journaled transaction.
	_ = e.Apply("rename-note", func(d *task.Doc, rec *Recorder) error {
		for _, t := range d.Tasks() {
			if nl, ch := links.RewriteLine(t.Text, oldRel, newBase); ch {
				rec.Before(t)
				t.SetText(nl)
				updated++
			}
		}
		return nil
	})

	// Rewrite note files at the raw-byte level so frontmatter and any keys nt
	// doesn't model are preserved (not re-serialized through note.Save).
	cur, _ := note.List(e.S)
	for _, x := range cur {
		data, e2 := store.ReadFile(x.Path)
		if e2 != nil {
			continue
		}
		if nl, ch := links.RewriteLine(string(data), oldRel, newBase); ch {
			if store.WriteAtomic(x.Path, []byte(nl), 0o644) == nil {
				updated++
			}
		}
	}
	return newRel, updated, nil
}

// UnlinkNote strips every inbound [[link]] to the target note across tasks.txt
// and notes/, replacing each with its plain display text, so deleting the note
// leaves no dangling links. Returns how many lines/files were rewritten. Like
// RenameNote it is not a single undo transaction.
func (e *Engine) UnlinkNote(target *note.Note) (updated int, err error) {
	rel := target.Rel
	if rel == "" {
		if r, e2 := filepath.Rel(e.S.NotesDir(), target.Path); e2 == nil {
			rel = filepath.ToSlash(r)
		}
	}
	_ = e.Apply("unlink-note", func(d *task.Doc, rec *Recorder) error {
		for _, t := range d.Tasks() {
			if nl, ch := links.StripLine(t.Text, rel); ch {
				rec.Before(t)
				t.SetText(nl)
				updated++
			}
		}
		return nil
	})
	cur, _ := note.List(e.S)
	for _, x := range cur {
		if x.Path == target.Path {
			continue
		}
		data, e2 := store.ReadFile(x.Path)
		if e2 != nil {
			continue
		}
		if nl, ch := links.StripLine(string(data), rel); ch {
			if store.WriteAtomic(x.Path, []byte(nl), 0o644) == nil {
				updated++
			}
		}
	}
	return updated, nil
}

// TrashNote moves a note file into the store's .trash/ (recoverable by hand).
// Like RenameNote/Archive it is a file move, not a journaled undo transaction;
// callers resolve inbound links first (UnlinkNote) when they don't want dangles.
func (e *Engine) TrashNote(n *note.Note) error {
	trash := filepath.Join(e.S.Dir, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	rel := n.Rel
	if rel == "" {
		if r, err := filepath.Rel(e.S.NotesDir(), n.Path); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	dest, err := trashDest(trash, rel)
	if err != nil {
		return err
	}
	return os.Rename(n.Path, dest)
}

// trashDest picks a free filename inside .trash/ for rel and claims it
// atomically (O_CREATE|O_EXCL), so the subsequent os.Rename can only ever land
// on a name nothing else holds.
//
// This matters because os.Rename silently overwrites its destination, and
// .trash/ is the ONLY recovery path for a trashed note — TrashNote is
// deliberately not a journaled undo transaction, so a clobbered copy is gone
// for good. Two collisions are reachable in normal use: deleting the same rel
// twice (create → delete → recreate → delete), and two distinct notes whose
// paths flatten to the same name (`a/b.md` and `a_b.md` both → `a_b.md`).
// `nt gc --yes` trashes in a loop, so a single run could destroy several notes
// while reporting only "collected N note(s) → .trash/ (recoverable)".
//
// Colliding names get a -2, -3, … suffix before the extension.
func trashDest(trash, rel string) (string, error) {
	flat := strings.ReplaceAll(rel, "/", "_")
	stem, ext := flat, ""
	if e := filepath.Ext(flat); e != "" {
		stem, ext = strings.TrimSuffix(flat, e), e
	}
	for i := 1; i <= 10000; i++ {
		cand := filepath.Join(trash, stem+ext)
		if i > 1 {
			cand = filepath.Join(trash, fmt.Sprintf("%s-%d%s", stem, i, ext))
		}
		f, err := os.OpenFile(cand, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close() // reclaimed by the rename below
			return cand, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("trash: no free name for %q after 10000 attempts", flat)
}

func base(rel string) string {
	return strings.TrimSuffix(filepath.Base(rel), ".md")
}

// slugRel slugs every segment of a notes-relative path, preserving the folder
// structure and the .md suffix, so a destination typed in human form lands on
// the same shape of filename `nt note` would have produced from a title.
func slugRel(rel string) string {
	stem := strings.TrimSuffix(rel, ".md")
	parts := strings.Split(stem, "/")
	for i, p := range parts {
		if s := note.Slug(p); s != "" {
			parts[i] = s
		}
	}
	return strings.Join(parts, "/") + ".md"
}
