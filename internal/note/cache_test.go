package note

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/navbytes/nt/internal/store"
)

func TestCacheReusesUnchangedReReadsChangedEvictsDeleted(t *testing.T) {
	s := testStore(t)
	write(t, s, "a.md", "# A\n\nalpha\n")
	write(t, s, "b.md", "# B\n\nbeta\n")
	c := NewCache()

	first, err := c.List(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("want 2 notes, got %d", len(first))
	}

	// No changes → the same *Note pointers come back (the parse was reused).
	second, _ := c.List(s)
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("note %q should be reused from cache (same pointer)", first[i].Rel)
		}
	}

	// Editing a.md must re-parse it: new pointer, new content.
	write(t, s, "a.md", "# A\n\nalpha EDITED, now a good deal longer than before\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(s.NotesDir(), "a.md"), future, future); err != nil {
		t.Fatal(err)
	}
	third, _ := c.List(s)
	var a *Note
	for _, n := range third {
		if n.Rel == "a.md" {
			a = n
		}
	}
	if a == nil || a == first[0] {
		t.Fatal("a.md should be re-parsed after an edit (a fresh pointer)")
	}
	if !strings.Contains(a.Body, "EDITED") {
		t.Errorf("re-parsed note should carry the new content, got %q", a.Body)
	}

	// Deleting b.md evicts it from both the listing and the cache.
	if err := os.Remove(filepath.Join(s.NotesDir(), "b.md")); err != nil {
		t.Fatal(err)
	}
	fourth, _ := c.List(s)
	if len(fourth) != 1 || fourth[0].Rel != "a.md" {
		t.Fatalf("after delete want only a.md, got %+v", relsOf(fourth))
	}
	c.mu.Lock()
	_, stillCached := c.entries[filepath.Join(s.NotesDir(), "b.md")]
	c.mu.Unlock()
	if stillCached {
		t.Error("deleted note should be evicted from the cache")
	}
}

func relsOf(ns []*Note) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Rel
	}
	return out
}

func benchStore(b *testing.B, n int) *store.Store {
	b.Helper()
	s := &store.Store{Dir: b.TempDir()}
	if err := os.MkdirAll(s.NotesDir(), 0o755); err != nil {
		b.Fatal(err)
	}
	body := strings.Repeat("Some note body content with a [[link]] and #tag.\n", 20)
	for i := 0; i < n; i++ {
		p := filepath.Join(s.NotesDir(), fmt.Sprintf("note-%04d.md", i))
		if err := os.WriteFile(p, []byte("# Note\n\n"+body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

// BenchmarkColdList is the old behavior: read+parse every note each call.
func BenchmarkColdList(b *testing.B) {
	s := benchStore(b, 2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := List(s); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCachedList is a warm cache with no changes: stat every note, re-parse
// none. This is the per-edit rebuild cost at scale.
func BenchmarkCachedList(b *testing.B) {
	s := benchStore(b, 2000)
	c := NewCache()
	if _, err := c.List(s); err != nil { // warm
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.List(s); err != nil {
			b.Fatal(err)
		}
	}
}
