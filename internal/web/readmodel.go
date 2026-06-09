package web

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/ulid"
)

// snapshot is the web adapter's in-memory read-model: the whole store parsed
// once, with the link graph (backlinks, task references, forward adjacency,
// orphan set) precomputed. Every read handler serves from it instead of
// re-walking notes/, re-parsing every .md, and shelling out to ripgrep per
// request (the per-request cost that did not scale past a few thousand
// notes). It is treated immutable once built: a
// rebuild constructs a fresh value and swaps the pointer under the Server lock.
type snapshot struct {
	doc      *task.Doc
	notes    []*note.Note          // every note on disk (archived included)
	active   []*note.Note          // notes minus archived — the working set (tree, grid count, state count)
	byHandle map[string]*note.Note // id AND rel → note (replaces findByHandle scan)
	byPath   map[string]*note.Note
	index    []linkRow // flat ⌘K palette index (active only)

	backlinks map[string][]Backlink // note path → note-source backlinks (de-duped)
	taskRefs  map[string][]TaskRef  // note path → tasks linking to it
	linked    map[string]bool       // note path → has any inbound link (note or task), for orphans
	fwd       map[string][]string   // note path → resolved outbound note paths, for the graph

	activity []activityEvent // every change, newest first (the provenance timeline)
	sources  []string        // distinct sources seen, sorted (for the source filter)

	readErr string // non-empty when reading tasks/notes failed (surfaced as a UI warning)

	builtAt time.Time
}

// activityEvent is one entry in the provenance timeline: who changed what, when.
// It's the view nt can build but pure-PKM tools can't — because nt has agents
// writing into the same store a human reads.
type activityEvent struct {
	When   time.Time `json:"when"`
	Action string    `json:"action"` // "added" | "updated" | "completed"
	Kind   string    `json:"kind"`   // "note" | "task"
	Source string    `json:"source"` // claude | cli | web | tui | …
	Title  string    `json:"title"`
	URL    string    `json:"url,omitempty"` // /n/<id> for notes; "" for tasks (no task page yet)
}

// flatNotes returns the flat ⌘K palette index for the snapshot.
func flatNotes(notes []*note.Note) []linkRow {
	out := make([]linkRow, 0, len(notes))
	for _, n := range notes {
		out = append(out, linkRow{URL: "/n/" + url.PathEscape(noteHandle(n)), Title: n.Title, Path: n.Rel})
	}
	return out
}

// buildSnapshot reads the store once and precomputes the link graph. The
// resolution it uses (links.Resolve over the in-memory notes/doc) is the exact
// same resolution the CLI/TUI/MCP use, so the rendered pages are byte-identical
// to the old per-request path — only faster.
func buildSnapshot(eng *mutate.Engine, cache *note.Cache) *snapshot {
	doc, docErr := eng.Read()
	notes, notesErr := cache.List(eng.S)

	// Surface read failures instead of silently rendering an empty store — a
	// corrupt tasks.txt or unreadable notes/ otherwise looks like "no data".
	readErr := ""
	switch {
	case docErr != nil:
		readErr = "couldn't read tasks: " + docErr.Error()
	case notesErr != nil:
		readErr = "couldn't read notes: " + notesErr.Error()
	}

	// active excludes archived notes: they stay in snap.notes (so the grid, the
	// note view, and unarchive can reach them) but drop out of the ⌘K index and
	// the link graph below — so the sidebar, graph, orphans, and backlinks show
	// only the working set.
	active := note.Active(notes)

	s := &snapshot{
		doc:       doc,
		notes:     notes,
		active:    active,
		byHandle:  make(map[string]*note.Note, len(notes)*2),
		byPath:    make(map[string]*note.Note, len(notes)),
		index:     flatNotes(active),
		backlinks: map[string][]Backlink{},
		taskRefs:  map[string][]TaskRef{},
		linked:    map[string]bool{},
		fwd:       map[string][]string{},
		readErr:   readErr,
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
	// same context the old ripgrep-backed path showed. Iterating `active` (not
	// `notes`) keeps archived notes out of the graph and backlinks; an archived
	// target is filtered below so it can't become a node via an inbound link.
	for _, src := range active {
		seenTarget := map[string]bool{} // one backlink entry per (source, target)
		fwdSeen := map[string]bool{}
		for _, line := range strings.Split(src.Body, "\n") {
			for _, raw := range links.Wikilinks(line) {
				it, ok := links.Resolve(raw, doc, notes)
				if !ok || it.Kind != "note" || it.Path == src.Path {
					continue
				}
				if tn := s.byPath[it.Path]; tn != nil && tn.Archived {
					continue // an inbound link must not revive an archived note as a node
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
				if tn := s.byPath[it.Path]; tn != nil && tn.Archived {
					continue // a task linking an archived note doesn't revive it
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

	s.buildActivity()
	return s
}

// buildActivity derives the provenance timeline from data already in the store:
// note Created/Updated timestamps, task creation times (decoded from the ULID),
// and task completion dates — each tagged with its source.
func (s *snapshot) buildActivity() {
	srcSeen := map[string]bool{}
	src := func(v string) string {
		if v == "" {
			v = "unknown"
		}
		srcSeen[v] = true
		return v
	}
	for _, n := range s.notes {
		url := "/n/" + url.PathEscape(noteHandle(n))
		if ts, err := time.Parse(time.RFC3339, n.Created); err == nil {
			s.activity = append(s.activity, activityEvent{When: ts, Action: "added", Kind: "note", Source: src(n.Source), Title: n.Title, URL: url})
		}
		if n.Updated != "" && n.Updated != n.Created {
			if ts, err := time.Parse(time.RFC3339, n.Updated); err == nil {
				s.activity = append(s.activity, activityEvent{When: ts, Action: "updated", Kind: "note", Source: src(n.Source), Title: n.Title, URL: url})
			}
		}
	}
	if s.doc != nil {
		for _, t := range s.doc.Tasks() {
			title := cleanTaskText(t.Text)
			if ts, ok := ulid.Time(t.ID()); ok {
				s.activity = append(s.activity, activityEvent{When: ts, Action: "added", Kind: "task", Source: src(t.Source()), Title: title})
			}
			if t.Done && t.Completed != "" {
				if ts, err := time.Parse("2006-01-02", t.Completed); err == nil {
					s.activity = append(s.activity, activityEvent{When: ts, Action: "completed", Kind: "task", Source: src(t.Source()), Title: title})
				}
			}
		}
	}
	sort.Slice(s.activity, func(i, j int) bool { return s.activity[i].When.After(s.activity[j].When) })
	for k := range srcSeen {
		s.sources = append(s.sources, k)
	}
	sort.Strings(s.sources)
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
