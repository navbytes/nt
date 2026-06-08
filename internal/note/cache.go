package note

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/navbytes/nt/internal/store"
)

// Cache is an mtime-keyed parse cache for note files. List walks notes/ but only
// re-reads and re-parses files whose size or mtime changed since last time —
// turning a snapshot rebuild from "read+parse every note" into "stat every note,
// read+parse the few that changed". This is what lets the in-memory read-model
// scale to thousands of notes (a single edit no longer re-reads the whole store).
//
// Returned *Note values are shared with the cache; the read-model treats them as
// read-only, so this is safe. Cache is safe for concurrent use.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	mtimeNs int64
	size    int64
	note    *Note
}

// NewCache returns an empty note cache.
func NewCache() *Cache { return &Cache{entries: map[string]entry{}} }

// List returns all notes under notes/, reusing unchanged files from the cache
// and re-parsing only those that were added or modified. Deleted files are
// evicted. Output ordering matches note.List (by Rel).
func (c *Cache) List(s *store.Store) ([]*Note, error) {
	dir := s.NotesDir()
	seen := map[string]struct{}{}
	var out []*Note
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries, like note.List
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
		info, e := d.Info()
		if e != nil {
			return nil
		}
		seen[path] = struct{}{}
		mtimeNs, size := info.ModTime().UnixNano(), info.Size()

		c.mu.Lock()
		ent, ok := c.entries[path]
		c.mu.Unlock()
		if ok && ent.mtimeNs == mtimeNs && ent.size == size {
			out = append(out, ent.note) // unchanged — reuse the parse
			return nil
		}

		n, le := Load(path)
		if le != nil {
			return nil
		}
		if rel, re := filepath.Rel(dir, path); re == nil {
			n.Rel = filepath.ToSlash(rel)
		}
		c.mu.Lock()
		c.entries[path] = entry{mtimeNs: mtimeNs, size: size, note: n}
		c.mu.Unlock()
		out = append(out, n)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Evict notes that no longer exist on disk.
	c.mu.Lock()
	for p := range c.entries {
		if _, ok := seen[p]; !ok {
			delete(c.entries, p)
		}
	}
	c.mu.Unlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}
