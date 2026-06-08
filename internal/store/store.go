// Package store resolves the nt data directory ($NT_DIR) and the paths of the
// files within it. The store is global by design (SPEC §2): one directory,
// always available regardless of the current working directory.
package store

import (
	"os"
	"path/filepath"
)

// Store is a handle to a resolved nt data directory.
type Store struct {
	Dir string
}

// Open resolves the store directory and ensures it (and notes/) exist.
//
// Resolution order:
//  1. $NT_DIR if set
//  2. $XDG_DATA_HOME/nt
//  3. ~/.local/share/nt
func Open() (*Store, error) {
	dir, err := ResolveDir()
	if err != nil {
		return nil, err
	}
	s := &Store{Dir: dir}
	if err := os.MkdirAll(s.NotesDir(), 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

// ResolveDir returns the store directory ($NT_DIR, else $XDG_DATA_HOME/nt, else
// ~/.local/share/nt) WITHOUT creating it — for callers (config, web) that need
// the path before, or without, opening the store.
func ResolveDir() (string, error) {
	if dir := os.Getenv("NT_DIR"); dir != "" {
		return dir, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "nt"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "nt"), nil
}

func (s *Store) TasksFile() string { return filepath.Join(s.Dir, "tasks.txt") }
func (s *Store) DoneFile() string  { return filepath.Join(s.Dir, "done.txt") }
func (s *Store) UndoFile() string  { return filepath.Join(s.Dir, "undo.jsonl") }
func (s *Store) LockFile() string  { return filepath.Join(s.Dir, "tasks.txt.lock") }
func (s *Store) NotesDir() string  { return filepath.Join(s.Dir, "notes") }
func (s *Store) LogFile() string   { return filepath.Join(s.Dir, "nt.log") }

// IsFresh reports whether the store has no tasks file and no notes yet — used
// to decide whether to run first-run onboarding (SPEC §10).
func (s *Store) IsFresh() bool {
	if _, err := os.Stat(s.TasksFile()); err == nil {
		return false
	}
	entries, err := os.ReadDir(s.NotesDir())
	if err != nil {
		return true
	}
	return len(entries) == 0
}
