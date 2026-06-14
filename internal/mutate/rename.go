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
	dest := filepath.Join(trash, strings.ReplaceAll(rel, "/", "_"))
	return os.Rename(n.Path, dest)
}

func base(rel string) string {
	return strings.TrimSuffix(filepath.Base(rel), ".md")
}
