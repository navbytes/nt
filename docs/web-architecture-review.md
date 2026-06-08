# Architecture review — does `nt` need to change to make `nt web` industry-leading?

- **Status:** External consultant review (commissioned 2026-06-08)
- **Scope:** all layers — domain, storage, write/concurrency model, web adapter, desktop path
- **Companion docs:** [ADR 0001](adr/0001-web-frontend-and-desktop.md) ·
  [roadmap](web-industry-leading.md) · [read-model plan](web-read-model-plan.md) ·
  [SPEC](../SPEC.md)

> Independent second opinion. The brief: audit the codebase and judge whether the
> *underlying* architecture must change to make `nt web` industry-leading —
> explicitly pressure-testing ADR 0001 (written for a viewer, not a leading app)
> and the roadmap doc, not rubber-stamping them.

## 1. Verdict

**Targeted changes needed — and they are all additive at the existing hexagonal
seam. No re-architecture, no rewrite, no point of no return is crossed.** The
domain split (`task`/`note`/`links`/`search`/`store`/`mutate`) is sound and is
the asset that makes this cheap. Plain files stay the source of truth. The work
concentrates in three places, none touching the domain core:

1. a read-side in-memory snapshot/index in `internal/web` to kill the
   read-the-whole-store-per-request cost;
2. closing two correctness gaps the web *write* path exposes — note saves
   bypass the lock/undo discipline and (until this review) had **no lost-update
   guard**;
3. a frontend delivery change (htmx + targeted vendored ESM, **not** an SPA)
   plus a smarter push model than "reload everything."

**CRDTs are not warranted. An SPA is not warranted now.** The ADR's core
decision is correct; the roadmap doc is mostly right but naive in three specific
places called out below.

## 2. Reasoning (grounded in the code)

**The hexagonal claim is real, not marketing.** Every adapter funnels through one
engine. The MCP agent path (`internal/mcp/mcp.go:220` `add`, `:251` `done`,
`:284` `update`) and the CLI both call `mutate.Engine.Apply`
(`internal/mutate/mutate.go:94`), which does the correct thing: acquire flock
(`internal/lock/lock.go:28`), **re-read from disk**, parse, apply,
journal-before-write (`mutate.go:118`), atomic rename (`store/atomic.go:12`).
This is a textbook lost-update-safe design that already handles "human + agent
writing simultaneously" **for tasks**. The web `/tasks` view is read-only today
(`server.go:475`), so the moment it becomes interactive (the roadmap's headline
Tier-1 item), task writes get this safety **for free** by routing through
`s.eng.Apply`. *This is the single most important architectural fact in the
engagement: the concurrency model the roadmap worries about is already solved for
tasks; the web just hasn't used it yet.*

**The note write path is weaker than the roadmap implied.** `handleSave`
(`server.go:299`) read the raw file at edit-open (`?raw=1`) and on POST called
`store.WriteAtomic` directly — two real problems:

- **No lost-update guard.** Between open and save, an agent (`nt_tag`/`nt_note`,
  `mcp.go:507`/`:317`) or `nt retag` can rewrite that `.md`; the browser save
  blindly clobbered it. SPEC §6's whole thesis is "never write your whole
  in-memory state back without re-reading." This is fine for a viewer with
  occasional edits (the ADR's original scope), not fine for an industry-leading
  editor with a concurrent agent. **This is the one place the multi-actor
  scenario actually bites, and it bites notes, not tasks.** *(Fixed in this
  review — see §addendum.)*
- **Note writes are outside the undo journal** (journal is task-ULID-keyed,
  `undo/undo.go`), so a web note edit is unrecoverable via `nt undo`. Acceptable
  for a viewer; a gap for a leading editor.

  Not a flock problem: notes are deliberately one-file-each with no shared lock
  (`note.go:2`), and atomic rename means readers never see a torn file. The
  issue is purely **last-writer-wins with no detection**.

**The performance issue is real, and more precisely: the read path is the
problem, not the domain.** Every request calls `s.load()` (`server.go:130`) →
`note.List` → a full `WalkDir` that opens, reads, and parses every `.md`
(`note.go:314-345`) → rebuilds the folder tree (`buildTree`) and flat index. The
link-heavy views are worse: `backlinksFor`/`links.Backlinks` (`links.go:194`)
shells out to ripgrep across the **entire store on every note page load**;
`tasksReferencing` (`render.go:231`) calls `links.Resolve`, which loops all notes,
for every link in every task; `handleGraph` (`server.go:505`) is O(notes × links
× notes). At SPEC's stated "hundreds of notes" this is sub-millisecond and fine.
At the **5,000-note Obsidian bar the roadmap sets as the goal**, a note page that
re-walks the disk, re-parses 5,000 files, and runs a full-store ripgrep per
request will be visibly sluggish. You cannot be industry-leading and re-read the
world per navigation. **This changes in the adapter, not the domain.**

### Where the consultant disagreed with the roadmap doc

1. **"Cache invalidated by the fsnotify watcher" is underspecified to the point
   of a bug factory.** The web watcher (`server.go:646`) does **not** debounce
   (SPEC §6.5 requires 50–100 ms debounce + self-write ignore) and does not tag
   self-writes. Hang naive invalidation off it, then enable editing, and you get
   reload storms + cache thrash on every save (editor writes → fsnotify fires →
   invalidate → SSE broadcast → the editing client reloads mid-edit). The cache
   is right; the doc skipped the hard part.
2. **Tasks vs. notes difficulty is reversed.** The roadmap treats interactive
   tasks as the risky part and editing as easier. Backwards: **tasks are the
   easy, safe win** (they ride `Apply`'s re-read+lock+journal); **notes are where
   the unsolved concurrency/undo gap lives** — and the roadmap never named the
   note lost-update problem at all. That was the most important correctness item
   in the engagement and it was absent.
3. **CodeMirror 6 "no-build vendored ESM" is not nearly free.** Unlike mermaid
   (one file), CM6 is a constellation of `@codemirror/*` + `@lezer/*` packages
   you must pre-bundle out-of-tree into one ESM artifact (~400 KB+). Tolerable
   (preserves no-build-*in-repo*) but it's the one place the "no build" story
   gets an asterisk — gate it behind whether autocomplete actually demands it
   (split live-preview reusing `renderBody` does not).

### Where the consultant agreed, emphatically

**No SPA.** ADR reasoning (`0001:41-49`) holds and is reinforced: the entire
rewriteable surface is ~1,400 LOC of glue over a domain that already returns
clean structs. The product identity (SPEC §1: single static binary, no build,
offline, localhost) is a genuine **competitive moat** in the 2026 AI-memory
category (Basic Memory et al. have no GUI). Rewriting to React to reach parity
with where you already are would be strategic self-harm. The Wails finding
(`server.go:107` `Handler()`) is legitimate: the same `http.Handler` renders
natively, so desktop is additive and should not drive the frontend decision now.

## 3. Specific architectural decisions

**A. Storage / index — keep plain files as source of truth; add a rebuildable
in-memory read-model. Do NOT add SQLite as primary.** Build a snapshot struct in
`internal/web` (parsed notes, folder tree, flat index, backlink map, forward-link
adjacency), `sync.RWMutex`-guarded, rebuilt on debounced fsnotify events. The
backlink map turns the per-request full-store ripgrep into O(1). Memory for 5k
small notes is single-digit MB. An optional SQLite **FTS index as a pure derived
cache** is defensible later if ripgrep ranking proves insufficient — never as
source of truth (that re-introduces the data-trapped-behind-a-binary problem SPEC
§1 walked away from).

**B. Editing — split live-preview first; lost-update guard NOW; CRDTs not
warranted.** (1) Optimistic concurrency on note saves: ETag/mtime on `?raw=1`,
`If-Match` on POST, 409 on mismatch. (2) Replace the raw `<textarea>` with split
live-preview reusing `renderBody` (the documented "seam #3", `render.go:43`).
(3) Route note saves so they're journaled/undoable. (4) CodeMirror only if inline
`[[ ]]`/`#tag` autocomplete is demanded, as a vendored pre-bundled artifact.
*On CRDTs:* they solve concurrent character-level editing of one document by
multiple live cursors. nt is single-user-per-document; the concurrency is
human-vs-agent at **file** granularity. ETag/409 + atomic rename is the correct,
proportionate tool. CRDTs would add a huge dependency, non-plain-text shadow
state, and an editor model that fights Markdown-on-disk. Reassess only for
real-time multi-human collaboration (out of scope for a localhost tool).

**C. Rendering — htmx + targeted vendored ESM. Confirm ADR: no SPA.** Adopt htmx
(~14 KB) to replace the `innerHTML` string-building (`app.js:89,164,185,224` —
the ADR's named smell, confirmed) and to make interactive tasks/fragments clean.
Vendor heavy widgets (a force-graph to replace Mermaid `graph LR`, which collapses
past ~50 nodes, `server.go:505`) as single pre-bundled ESM files. An SPA is only
justified if you commit to (a) real-time multi-client collaboration or (b) a Wails
Go-bindings desktop app as a first-class product (ADR Option D) — neither a
current goal. The hexagonal seam lets you defer that decision indefinitely at
near-zero carrying cost.

**D. Real-time / push — evolve SSE from "reload everything" to typed events; keep
the transport.** Replace the single `data: reload` (`server.go:639`) with typed
events (`task-changed`, `note-changed:<id>`) so htmx swaps only the affected
fragment and the *editing* client can ignore its own writes. Requires the
debounce + self-write tagging that's currently missing (SPEC §6.5 mandates it;
`server.go:646` doesn't implement it).

**E. Read-the-whole-store-per-request — solved by (A).**

**F. Concurrent human + agent writes — tasks already safe via `Apply`; notes
fixed by (B).** Route web *task* mutations through `s.eng.Apply` (free safety);
add the ETag guard + journaling for *notes*. After that the multi-actor model is
sound end-to-end.

## 4. What NOT to change

- The hexagonal domain split — the reason this is additive, not a rewrite.
- `mutate.Engine.Apply`'s re-read-under-lock-then-journal contract
  (`mutate.go:94-122`) — genuinely well-engineered; reuse, don't reinvent.
- Plain files as source of truth — the moat in the AI-memory category. Any index
  stays a rebuildable shadow.
- flock + atomic rename for tasks — correct and sufficient for single-machine.
  Don't add a daemon or DB.
- No-CGO single static binary for the CLI; desktop isolated in a nested module.
- CSRF + `--edit` gating + localhost-only bind — don't add auth/network exposure.
- goldmark/Chroma reuse from the module graph — zero new render deps.

## 5. Sequencing / migration risk

Everything is additive at `internal/web` + the existing `mutate` API. **No point
of no return, no rewrite.** Order, each independently shippable:

1. **Correctness first (cheap, high-stakes):** ETag/`If-Match` lost-update guard
   on note save + route new web task mutations through `s.eng.Apply`.
2. **Read-model + debounced/self-write-aware watcher** (fixes perf, unblocks
   surgical SSE) — the one item with real subtlety; do it per SPEC §6.5.
3. **htmx adoption** (removes the smell, enables fragment swaps).
4. **Interactive tasks** → agent-memory home page / activity timeline (the wedge).
5. **Split live-preview editor**, then CodeMirror autocomplete only if demanded.
6. **Force-directed graph** as a vendored ESM artifact.

The only thing that creates a point of no return is an SPA + migrating logic into
the client. Don't — revisit only when desktop-as-product (ADR Option D) or
real-time collaboration becomes a committed goal.

## 6. Crisp recommendation

**Do not re-architect. Evolve `nt web` in place at the existing seam:** (1) add an
`If-Match`/ETag lost-update guard to note saves and route all new web writes
through `mutate.Apply`; (2) add a rebuildable in-memory read-model invalidated by
a *properly debounced, self-write-aware* fsnotify watcher to kill the per-request
full-store walk and per-page ripgrep so you hit the 5k-note bar; (3) adopt htmx +
typed SSE to make tasks interactive and updates surgical. Keep plain files,
single static binary; skip SQLite-as-primary, CRDTs, and the SPA. The ADR's
"no SPA, server-rendered, additive" decision survives the industry-leading
ambition; the roadmap was largely right but reversed tasks-vs-notes difficulty
and glossed over the note lost-update gap and the debounce requirement.

---

## Addendum — fixes already landed from this review

- **Note-save lost-update guard (decision B.1, sequencing step 1) — DONE.**
  `?raw=1` now returns a content-hash `ETag` (`server.go` `etag`); `handleSave`
  refuses a stale write with **409** when the client's `If-Match` no longer
  matches the bytes on disk; `app.js` captures the ETag and sends it back, with a
  "Changed on disk — reload to merge" message on 409. Covered by
  `TestEditingLostUpdateGuard`.
- **Read-model + debounced watcher (decision A/E, step 2)** — designed in
  [web-read-model-plan.md](web-read-model-plan.md); not yet implemented.
</content>
