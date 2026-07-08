package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/navbytes/nt/internal/store"
)

// cmdStoreHash prints a stable fingerprint of the note store: a digest over
// every note's path, size, and mtime. Its contract: the value changes iff a
// note file under notes/ was added, removed, or modified; it is otherwise
// stable, including across process restarts and machines (no timestamps or
// randomness of its own — only the files' own mtimes feed it).
//
// It's a single primitive meant for reuse: the OpenCode/Pi plugins use it to
// dedupe live injection against their file-mode snapshot and to skip
// recompiling the rules+memory block when nothing changed; a future note
// staleness guard and persisted index sidecar can use it too. Deliberately
// whole-store (not scoped to rules/+memory/ folders alone): a tag can mark any
// note "rule" regardless of folder, and one shared fingerprint is simpler than
// each consumer inventing its own directory walk — the tradeoff is that
// editing an unrelated note also changes the hash, which only costs an extra
// recompute, never a stale one.
//
//	nt store-hash
func cmdStoreHash(args []string) int {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("store-hash: no arguments (try `nt store-hash`)"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	hash, err := computeStoreHash(e.S)
	if err != nil {
		return fail(err)
	}
	fmt.Println(hash)
	return 0
}

// computeStoreHash walks notes/ with the same skip rules as note.List (hidden
// dirs like .trash/.obsidian/.git pruned, only *.md files) and hashes each
// file's slash-separated relative path, size, and mtime (nanoseconds where the
// filesystem provides that resolution). Entries are sorted by path before
// hashing so the result doesn't depend on directory-walk order.
func computeStoreHash(s *store.Store) (string, error) {
	dir := s.NotesDir()
	var entries []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries rather than aborting the walk
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		entries = append(entries, fmt.Sprintf("%s\x00%d\x00%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			// An uninitialized/empty store hashes to a stable, well-known value
			// rather than erroring — a fresh store is a valid state to fingerprint.
			entries = nil
		} else {
			return "", err
		}
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
