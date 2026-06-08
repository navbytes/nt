package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteAtomicRoundTrip: WriteAtomic creates the file, overwrites it on a
// second write, and leaves no temp files behind (the staging file is renamed,
// not left in the directory).
func TestWriteAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.txt")

	if err := WriteAtomic(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadFile(path); string(got) != "one\n" {
		t.Fatalf("first write: got %q", got)
	}

	if err := WriteAtomic(path, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadFile(path); string(got) != "two\n" {
		t.Fatalf("overwrite: got %q", got)
	}

	// No staging files (.nt-*.tmp) should remain in the directory.
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".tmp" || len(e.Name()) > 4 && e.Name()[:4] == ".nt-" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

// TestWriteAtomicPermissions: the written file carries the requested mode.
func TestWriteAtomicPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := WriteAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// TestReadFileMissingIsEmpty: a missing file reads as empty bytes, no error
// (a fresh store legitimately has no tasks.txt yet).
func TestReadFileMissingIsEmpty(t *testing.T) {
	got, err := ReadFile(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing file should read empty, got %q", got)
	}
}
