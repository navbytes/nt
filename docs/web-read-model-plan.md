# Implementation plan — `nt web` in-memory read-model + debounced watcher

- **Status:** Plan (no code yet)
- **Date:** 2026-06-08
- **Owner area:** `internal/web`
- **Tracks:** architecture-review §3.A/E (perf) and §3.D (typed push) ·
  [web-architecture-review.md](web-architecture-review.md)

## Goal

Make `nt web` snappy at the **5,000-note bar** (the Obsidian benchmark) by
serving every read from a single in-memory snapshot instead of re-walking and
re-parsing the whole store per request — without weakening any product value
(plain files stay source of truth, single static binary, no build, offline,
localhost). Secondary goal: make the fsnotify→SSE path **debounced and
self-write-aware** so it can later drive surgical fragment updates instead of
"reload everything."

## Why (the cost being removed)

Per request today (`internal/web/server.go`):

- `s.load()` (`server.go:130`) → `note.List` → `filepath.WalkDir` that **opens,
  reads, and parses every `.md`** (`internal/note/note.go:314-345`).
- `buildTree` (`server.go:698`) + `flatNotes` (`server.go:194`) rebuilt every
  time.
- Per **note page**: `backlinksFor` → `links.Backlinks` (`internal/links/links.go:194`)
  shells out to **ripgrep across the entire store**; `tasksReferencing`
  (`render.go:231`) calls `links.Resolve` (loops all notes) for every link in
  every task.
- `/graph` (`server.go:505`) is O(notes × links × notes).

At hundreds of notes: sub-millisecond, fine. At 5k notes: a single navigation
re-reads the world + a full-store grep — hundreds of ms to seconds. This is a
read-path problem; the domain is not at fault.

## Design

### 1. A snapshot type owned by the web adapter

Introduce an unexported `snapshot` (in a new `internal/web/readmodel.go`)
computed **once** from a store read and reused by all read handlers:

```
type snapshot struct {
    notes     []*note.Note            // parsed once (sorted by Rel, as note.List returns)
    doc       *task.Doc               // tasks parsed once
    byHandle  map[string]*note.Note   // id and Rel → note (replaces findByHandle loop)
    byPath    map[string]*note.Note
    tree      []*treeNode             // prebuilt folder tree (active-marking done per request)
    index     []linkRow               // flat ⌘K index (flatNotes)
    backlinks map[string][]links.Hit  // note path → inbound links (replaces per-page ripgrep)
    fwd       map[string][]string     // note path → resolved outbound note paths (graph adjacency)
    builtAt   time.Time
}
```

Key wins:
- **Backlink map** is the big one: built **once** by scanning each note/task body
  for `[[…]]` and resolving via `links.Resolve`, it turns every page's full-store
  ripgrep into an O(1) map lookup. (`links.Backlinks`' ripgrep is only needed
  because there's no index; the snapshot *is* the index.)
- `fwd` adjacency makes `/graph` O(edges), not O(notes² × links).
- `byHandle`/`byPath` replace linear `findByHandle`/`findByPath` scans.

### 2. Server holds it behind an RWMutex

```
type Server struct {
    ...
    mu   sync.RWMutex
    snap *snapshot
}
```

- `s.current()` takes `RLock`, returns the pointer (snapshots are treated
  immutable once built; rebuild swaps the pointer under `Lock`).
- Handlers replace `doc, notes := s.load()` with `snap := s.current()` and read
  `snap.notes` / `snap.doc` / the prebuilt maps.
- `tree` is shared; the per-request "current note" marking (`Current` field,
  `buildTree`) is applied as a cheap post-pass over a cloned spine or via a
  separate "activeHandle" carried into the template (preferred: stop mutating the
  shared tree — pass the active handle to the template and compare there, or clone
  only the ancestor chain).

### 3. Rebuild trigger: debounced + self-write-aware watcher

This is the part the architecture review flagged as the only real subtlety. The
current `watch()` (`server.go:646`) broadcasts on **every** raw fsnotify event,
with no debounce and no self-write filter. Replace its body (not its shape) with:

1. **Debounce** (SPEC §6.5: 50–100 ms). Coalesce a burst of events (atomic-rename
   writes fire `CREATE`+`RENAME`+`CHMOD`) into one rebuild via a reset timer.
2. **Self-write suppression.** When the web itself writes (note save, and later
   task mutations), record the path + a short grace window (or an mtime/size
   fingerprint of what we just wrote). Drop the matching fsnotify event so we
   don't rebuild+broadcast our own save and reload the editing client mid-edit.
   - Concretely: a small `recentWrites map[string]time.Time` set in `handleSave`
     (and future write handlers) immediately before `WriteAtomic`; the watcher
     skips an event whose path is in the set within the window.
3. On a surviving, debounced change: **rebuild the snapshot**, swap the pointer
   under `Lock`, then `hub.broadcast()`.

### 4. Build path

`buildSnapshot(eng)`:
1. `doc, _ := eng.Read()`; `notes, _ := note.List(eng.S)` (the existing reads —
   now done once per change, not per request).
2. Build `byHandle`/`byPath`, `tree` (`buildTree` with `activeHandle==""`),
   `index` (`flatNotes`).
3. Build `backlinks`: for each note and each task, walk `[[…]]` tokens
   (`links.Wikilinks` / `task.Links`), resolve with `links.Resolve` over the
   in-memory `notes`/`doc`, and append a `links.Hit{Path, Text}` to the target's
   slice. This reuses the exact resolution the CLI/TUI use — no behavior drift.
4. Build `fwd` adjacency from the same walk (note→note edges) for `/graph`.

`backlinksFor` / `tasksReferencing` / `graphSource` are refactored to read these
maps instead of recomputing. They keep their signatures where possible (pass the
snapshot in) so `render.go` stays a thin formatter.

### 5. Initial build & lifecycle

- Build the first snapshot in `Serve` / `StartWatch` **before** serving (so the
  first request is already warm).
- `NewServer` can build a snapshot eagerly too, so the Wails desktop embedder and
  tests get a populated server without special-casing.
- On rebuild failure (transient read error mid-write), keep the previous snapshot
  (never serve an empty store because a read raced a rename).

## Correctness & consistency notes

- **Plain files remain source of truth.** The snapshot is a pure derived cache;
  losing it (restart) costs only a rebuild. This is the same shadow-index
  principle the AI-memory tools (memsearch/sqlite-memory) converged on.
- **Writes still go through the domain.** Note saves keep using `store.WriteAtomic`
  (now ETag-guarded); future task writes go through `s.eng.Apply`. The snapshot is
  refreshed *after* the write via the watcher (or an explicit rebuild call on the
  write path for zero-latency self-updates — optional optimization).
- **Read-after-write for the writer.** Because self-writes are suppressed in the
  watcher, the writing handler should refresh its own view explicitly (rebuild, or
  reload the page) so the author sees their change immediately while *other*
  clients get the debounced broadcast.

## Typed SSE (follow-on, same change set)

Once the snapshot exists, evolve `handleSSE` (`server.go:621`) and the broadcast
from a bare `data: reload` (`server.go:639`) to typed events
(`task-changed` / `note-changed:<id>`), so a later htmx layer can swap only the
affected fragment and the editing client can ignore its own write events. The
debounce/self-write work above is the prerequisite; the typed payload is a small
additional step.

## Testing

- **Behavior parity:** existing `internal/web` tests must pass unchanged (the
  snapshot must produce byte-identical pages). Run `go test ./internal/web/`.
- **Backlink-map equivalence:** a test asserting snapshot backlinks match the
  current `links.Backlinks` ripgrep output for a fixture store.
- **Debounce:** a test that N rapid writes produce one rebuild/broadcast (count
  hub broadcasts under a burst).
- **Self-write suppression:** a save via `handleSave` does **not** trigger an SSE
  broadcast to other clients within the grace window (or triggers exactly the
  surgical event, not a full reload), while an *external* file write does.
- **Staleness bound:** after an external write + debounce window, `s.current()`
  reflects it.
- **Race:** `go test -race ./internal/web/` with concurrent reads + a rebuild.

## Rollout / risk

- **Fully additive**, contained to `internal/web` (+ no domain changes). The
  handlers' diffs are mechanical (`s.load()` → `s.current()` + map lookups).
- **Reversible:** if the read-model misbehaves, revert to `s.load()` per request;
  the domain is untouched.
- **Watch the subtlety:** debounce + self-write suppression is the only part with
  real concurrency nuance — implement and test it first, before hanging cache
  invalidation off it (the architecture review's explicit warning).

## Suggested order within this work item

1. Snapshot type + `buildSnapshot` + `s.current()`; switch read handlers to it
   (no watcher change yet — rebuild on every event, accept temporary thrash).
2. Backlink/adjacency maps; refactor `backlinksFor`/`tasksReferencing`/`graphSource`.
3. Debounced, self-write-aware watcher (replace `watch()` body).
4. Typed SSE events (optional in this item; can split out).
</content>
