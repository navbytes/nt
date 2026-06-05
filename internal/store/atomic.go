package store

import (
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path atomically: it writes a temp file in the same
// directory (so rename stays on one filesystem) then renames over the target.
// On POSIX, rename(2) is atomic, so a reader never sees a half-written file
// (SPEC §6.1).
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nt-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ReadFile reads a file, returning empty bytes (not an error) when it does not
// exist yet — tasks.txt may legitimately be absent on a fresh store.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}
