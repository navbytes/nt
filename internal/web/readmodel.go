package web

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// snapshot is the web adapter's in-memory read-model: the whole store parsed
// once, with the link graph (backlinks, task references, forward adjacency,
// orphan set) precomputed. Every read handler serves from it instead of
// re-walking notes/, re-parsing every .md, and shelling out to ripgrep per
// request (the per-request cost that did not scale past a few thousand notes —
// see docs/web-read-model-plan.md). It is treated immutable once built: a
// rebuild constructs a fresh value and swaps the pointer under the Server lock.
type snapshot struct {
	doc      *task.Doc
	notes    []*note.Note
	byHandle map[string]*note.Note // id AND rel → note (replaces findByHandle scan)
	byPath   map[string]*note.Note
	index    []linkRow // flat ⌘K palette index

	backlinks map[string][]Backlink // note path → note-source backlinks (de-duped)
	taskRefs  map[string][]TaskRef  // note path → tasks linking to it
	linked    map[string]bool       // note path → has any inbound link (note or task), for orphans
	fwd       map[string][]string   // note path → resolved outbound note paths, for the graph

	builtAt time.Time
}

// buildSnapshot reads the store once and precomputes the link graph. The
// resolution it uses (links.Resolve over the in-memory notes/doc) is the exact
// same resolution the CLI/TUI/MCP use, so the rendered pages are byte-identical
// to the old per-request path — only faster.
func buildSnapshot(eng *mutate.Engine) *snapshot {
	doc, _ := eng.Read()
	notes, _ := note.List(eng.S)

	s := &snapshot{
		doc:       doc,
		notes:     notes,
		byHandle:  make(map[string]*note.Note, len(notes)*2),
		byPath:    make(map[string]*note.Note, len(notes)),
		index:     flatNotes(notes),
		backlinks: map[string][]Backlink{},
		taskRefs:  map[string][]TaskRef{},
		linked:    map[string]bool{},
		fwd:       map[string][]string{},
		builtAt:   time.Now(),
	}
	for _, n := range notes {
		s.byPath[n.Path] = n
		if n.ID != "" {
			s.byHandle[n.ID] = n
		}
		s.byHandle[n.Rel] = n
	}

	// Note → note edges (backlinks, forward adjacency, "linked" set). Scanning
	// line by line lets each backlink carry its matching line as a snippet, the
	// same context the old ripgrep-backed path showed.
	for _, src := range notes {
		seenTarget := map[string]bool{} // one backlink entry per (source, target)
		fwdSeen := map[string]bool{}
		for _, line := range strings.Split(src.Body, "\n") {
			for _, raw := range links.Wikilinks(line) {
				it, ok := links.Resolve(raw, doc, notes)
				if !ok || it.Kind != "note" || it.Path == src.Path {
					continue
				}
				tgt := it.Path
				s.linked[tgt] = true
				if !fwdSeen[tgt] {
					fwdSeen[tgt] = true
					s.fwd[src.Path] = append(s.fwd[src.Path], tgt)
				}
				if !seenTarget[tgt] {
					seenTarget[tgt] = true
					s.backlinks[tgt] = append(s.backlinks[tgt], Backlink{
						Title:  src.Title,
						URL:    "/n/" + url.PathEscape(noteHandle(src)),
						Text:   snippet(line),
						IsNote: true,
					})
				}
			}
		}
	}

	// Task → note references (the task↔note moat) + orphan accounting.
	if doc != nil {
		for _, t := range doc.Tasks() {
			seenT := map[string]bool{} // one ref per (task, target)
			for _, raw := range t.Links() {
				it, ok := links.Resolve(raw, doc, notes)
				if !ok || it.Kind != "note" {
					continue
				}
				tgt := it.Path
				s.linked[tgt] = true
				if !seenT[tgt] {
					seenT[tgt] = true
					s.taskRefs[tgt] = append(s.taskRefs[tgt], TaskRef{
						Text:   cleanTaskText(t.Text),
						Status: t.Status(),
						Source: t.Source(),
					})
				}
			}
		}
	}
	return s
}

// findHandle resolves a note by id or rel from the prebuilt map.
func (s *snapshot) findHandle(h string) *note.Note { return s.byHandle[h] }

// ---- self-write suppression -------------------------------------------------

// selfWriteWindow is how long after the adapter writes a file its own fsnotify
// event is ignored, so a save doesn't bounce the editing client or trigger a
// redundant rebuild+broadcast (the synchronous rebuild on the write path keeps
// the writer's own next read fresh).
const selfWriteWindow = 750 * time.Millisecond

type writeTracker struct {
	mu     sync.Mutex
	recent map[string]time.Time
}

func newWriteTracker() *writeTracker { return &writeTracker{recent: map[string]time.Time{}} }

func (wt *writeTracker) mark(path string) {
	wt.mu.Lock()
	wt.recent[path] = time.Now()
	wt.mu.Unlock()
}

func (wt *writeTracker) isSelf(path string) bool {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	t, ok := wt.recent[path]
	if !ok {
		return false
	}
	if time.Since(t) < selfWriteWindow {
		return true
	}
	delete(wt.recent, path)
	return false
}
