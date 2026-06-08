# Making `nt web` industry-leading — research & roadmap

- **Status:** Research / proposal (no code changes)
- **Date:** 2026-06-08
- **Scope:** `internal/web` (the `nt web` browser UI) and its desktop path
- **Companion docs:** [ADR 0001](adr/0001-web-frontend-and-desktop.md) ·
  [SPEC §12.1](../SPEC.md) · [`desktop/`](../desktop/README.md)

## TL;DR

The fastest path to "industry-leading" is **not** to out-Obsidian Obsidian. nt
will lose a feature-count race against a 2,000-plugin ecosystem, and chasing it
would force us to abandon the things that actually make nt special (plain files,
single static binary, no build step, fully offline, localhost-only).

The winning move is to **own the category nt already lives in**: the human-facing
GUI for an *AI-managed* task + note memory layer. A whole class of 2026 tools —
Basic Memory, sqlite-memory, memsearch, agentmemory — store agent memory as
Markdown + MCP but have **no GUI of their own**; they tell users to "just open it
in Obsidian." nt ships the CLI, the TUI, the MCP server, *and* the GUI over one
shared domain. No competitor in the AI-memory category has that. `nt web` should
become the best window that exists into what your agents know and are doing —
while reaching table-stakes parity on editing, tasks, search, and graph so it's
also a perfectly good notes app on its own.

This document inventories what's there today, benchmarks it against the field,
and proposes a tiered roadmap that stays inside nt's product values.

> **Implementation status (2026-06-08).** Tier 0 (htmx, typed SSE, in-memory
> read-model + debounced watcher) and the headline Tier 1–2 items are **shipped**:
> interactive tasks routed through `mutate.Apply`, the note lost-update guard, a
> split live-preview editor, and an interactive force-directed graph. Remaining:
> note/folder creation from the web, ranked search with highlighted snippets +
> task search, the activity/provenance timeline, the agent-memory home page, the
> web Logbook, properties editing, split panes, and all of Tier 3. See the
> per-item ✅ markers below and [web-architecture-review.md](web-architecture-review.md).

---

## 1. What `nt web` is today (honest inventory)

`internal/web` is a thin HTTP adapter (~1,050 LOC Go + ~1,130 LOC of
HTML/CSS/JS) over the same `task`/`note`/`links`/`search`/`store`/`mutate`
domain the CLI, TUI, and MCP server use. It is **server-rendered HTML +
`go:embed`, zero external requests, fully offline.** Already shipped:

| Capability | Where | Notes |
|---|---|---|
| Folder-tree sidebar | `server.go` `buildTree` | rebuilt per request; collapse state in `localStorage` |
| Markdown render (GFM) | `render.go` `md` | goldmark, auto heading IDs, safe (raw HTML escaped) |
| `[[wikilink]]` nav | `render.go` `rewriteWikilinks` | resolves to stable `/n/<ULID>`; unresolved → "did you mean" page |
| Backlinks + "Referenced by tasks" | `render.go` | note↔note and task↔note moats |
| Full-text search | `handleSearch` | substring title match + ripgrep literal; search-as-you-type |
| Command palette (⌘K) | `app.js` | client-side fuzzy filter over a flat note index |
| `/tasks` dashboard | `handleTasks` | grouped by status, urgency-sorted — **read-only** |
| `/tags`, `/orphans`, `/graph` | handlers | tag cloud, orphan finder, Mermaid link graph |
| Mermaid diagrams | vendored gzip | only external asset, embedded, offline |
| Syntax highlighting | Chroma | class-based, theme-scoped (light/dark) |
| Themes + reading width | `app.js` | Tokyo Night, persisted |
| Live reload | SSE + fsnotify | `handleSSE` / `watch` |
| Hover link previews, TOC, scrollspy, copy buttons, recently-viewed | `app.js` | progressive enhancement |
| Opt-in editing (`--edit`) | `handleSave` + `app.js` | **raw-file `<textarea>`**, CSRF-guarded, atomic write |
| Desktop shell | `desktop/` | Wails wraps the *same* `http.Handler` (spike, distributable) |

This is already a strong v1. The gaps below are what separate "strong viewer"
from "industry-leading app."

---

## 2. Where the bar is (competitive landscape, 2026)

Two markets are relevant, and nt straddles both.

**A. Plain-text PKM (Obsidian / Logseq).** The bar here is: a live WYSIWYG-ish
Markdown editor with inline `[[ ]]`/`#tag` autocomplete and slash commands; an
interactive **force-directed** graph (pan/zoom, local/neighbor graph, filters)
— Obsidian's signature, now even 3D via plugins; daily notes / journal;
properties (structured frontmatter) editing; split panes; and snappy
performance past 5,000 notes. Obsidian wins on document-first prose + a huge
plugin ecosystem; Logseq on outliner/block-reference + journal-first capture.

**B. AI agent memory (the hot 2026 category).** Basic Memory, sqlite-memory,
memsearch, agentmemory: Markdown-as-source-of-truth, MCP-connected, semantic/
hybrid search, "the AI and you read/write the same files." Their consistent
**weakness is the human surface** — they explicitly defer the GUI to Obsidian.

nt is the rare tool sitting in **both**: it's a Markdown PKM *and* an MCP/agent
memory store, with a first-party GUI. That overlap is the strategy.

---

## 3. The strategic wedge — be the GUI for AI memory

Everything in the AI-memory category is missing exactly what nt already has
(a GUI) and could make great. So the highest-differentiation features are ones
**no PKM tool ships** because they don't have an agent writing into the store:

1. **Activity / provenance timeline.** A reverse-chronological feed of what
   changed and *who* changed it — `src:claude` vs human — across tasks and
   notes. nt already records `src:` and `created`/`completed`; the web has the
   data and shows none of this stream. This is the "what did my agent do while
   I was away" view. Nobody else can build it.
2. **Source/provenance filtering everywhere.** Filter tasks, notes, search, and
   the graph by source. "Show me only what Claude captured this week."
3. **A live agent-memory dashboard as the home page.** `nt ready` (open,
   unblocked, urgency-sorted) is the agent's "start here" — surface it as the
   landing view: what's ready, what's blocked and why, what's in flight
   (`doing`), recently completed (the Logbook, which today only exists in the
   TUI).
4. **Bi-directional, of course.** Because nt owns the write path (`mutate`),
   the GUI can *act*: complete a task, re-prioritize, add a follow-up — and the
   next agent session reads it back. The memory loop closes in the browser.

Lean into this and `nt web` becomes a category-definer, not a me-too notes app.

---

## 4. Gap analysis → prioritized roadmap

Tiers are ordered by *impact ÷ cost*, staying inside the ADR's no-build,
single-binary, offline constraints.

### Tier 0 — Foundations the rest depends on

- **A real JSON API + fragment endpoints.** The `?json=1`/`?preview=1`/`?raw=1`
  query params already hint at this. Formalize a small read/write API
  (tasks, notes, search, activity) so both htmx fragment-swaps and a future
  client/desktop can share it. (ADR Decision 2 / Option D groundwork.)
- **Adopt htmx (~14 KB, vendored) + optionally Alpine.js (~15 KB).** The ADR
  already names this as the chosen direction. It removes the `innerHTML`
  string-building smell in `app.js` (palette, search, recent) and is the
  prerequisite for interactive tasks/editing without an SPA. Keeps every
  product value: single file, no build, offline.
- **Performance pass — a *rebuildable read-model*, not a naive cache.** Today
  every request calls `s.load()` (reads the whole store) and rebuilds the folder
  tree (`buildTree`) and note index; link views run a full-store ripgrep *per
  page load* (`links.Backlinks`). Fine at hundreds of notes, not at 5,000+. Add
  an in-memory snapshot (parsed notes + tree + flat index + **backlink map** +
  link adjacency) invalidated by the fsnotify watcher. **Caveat (per the
  architecture review): the current watcher (`server.go:646`) has no debounce and
  no self-write suppression — SPEC §6.5 requires both. A naive cache hung off it
  causes reload storms and reloads the editing client mid-save. Do this as a
  proper debounced, self-write-aware read-model** — see
  [web-read-model-plan.md](web-read-model-plan.md). This is the difference
  between "snappy past 5k notes" (the Obsidian bar) and not.

### Tier 1 — Table stakes (close the obvious PKM gaps)

> **Correctness ordering (per the architecture review).** Do the *safe* write
> first, then the *risky* one — the reverse of how this was originally framed.
> **Tasks are the easy, safe win:** task writes ride `mutate.Engine.Apply`'s
> existing re-read-under-lock + undo-journal contract, so concurrent human+agent
> writes are already handled. **Notes are the harder problem:** note saves go
> straight to `store.WriteAtomic`, outside the journal, and the lost-update guard
> (now landed — ETag/`If-Match` → 409) was missing. Build interactivity on top of
> these guarantees, not before them.

- **Interactive tasks** (the biggest single gap given nt's identity — and the
  *safe* place to start). `/tasks` is read-only. Wire complete / reopen / set
  status / due / priority / add / add-follow-up via htmx POSTs **routed through
  `s.eng.Apply`** (which gives lock + re-read + undo for free), CSRF-guarded and
  gated like `--edit`. This is what turns the web from a viewer into the memory
  GUI.
- **Live Markdown editing, not a raw textarea** (the harder path — notes lack the
  task write guarantees, so pair it with the lost-update guard + journaling).
  The current editor is a plain `<textarea>` (`app.js` editing IIFE). The
  lost-update guard (ETag/`If-Match`) is **done**; still outstanding: route note
  saves so they're undoable. Upgrade the surface to either (a) a **split live
  preview** reusing the existing `renderBody` path verbatim (the ADR's "seam
  #3"), or (b) **CodeMirror 6** for syntax-highlighted editing with:
  - inline `[[ ]]` wikilink autocomplete (fed by the existing note index),
  - `#tag` / `+project` autocomplete (fed by `/tags`),
  - slash-command insert, `⌘S` save (already wired), conflict-safe atomic write
    (already implemented).
  Recommendation: start with split live-preview (cheapest, reuses Go render),
  layer CodeMirror autocomplete after. **Note: CodeMirror 6 is *not* a single
  vendored file like mermaid — it's many `@codemirror/*`/`@lezer/*` packages that
  must be pre-bundled out-of-tree into one ESM artifact (~400 KB+). That keeps the
  no-build-*in-repo* guarantee but adds an out-of-tree build step — gate it behind
  whether autocomplete is actually demanded.** Avoid TipTap/Milkdown — they assume
  an npm build and a ProseMirror document model, which fights "plain files, no
  build."
- **Note + folder creation from the web.** `handleSave` only writes existing
  notes. Add "New note" (and new folder), reusing `note.Create`. Right now you
  must drop to the terminal to start a note — a hard stop for GUI-first users.
- **Better search.** Add ranked results with **highlighted match snippets**, a
  full results page (not just the sidebar list), and filters (tag / folder /
  source / status). Extend search to **tasks**, not just notes. Consider a tiny
  in-memory inverted index built at load (rebuildable, plain files stay source
  of truth — same pattern memsearch/sqlite-memory use).

### Tier 2 — Signature / differentiating

- **Interactive force-directed graph.** Replace the Mermaid `graph LR` (which
  visually collapses past ~50 nodes and isn't pannable) with a vendored,
  no-build force-directed canvas (e.g. a single-file d3-force or
  force-graph ESM bundle, embedded like mermaid). Add: pan/zoom, **local graph**
  (neighbors of the current note in the sidebar), hover-highlight neighborhood,
  click-to-open, and **filter/color by tag, folder, and source**. This is
  Obsidian's signature feature *plus* the provenance angle only nt can do.
- **Activity / provenance timeline** (the wedge from §3). New `/activity` view +
  a compact "recent" panel on the home page. Source-badged, filterable, links
  into the items. Built from `created`/`completed`/`src:` already in the model.
- **Agent-memory home page.** Make `/` the dashboard: `nt ready` queue, in-flight
  (`doing`), blocked-with-reasons, recent activity, recently-viewed. Today `/`
  is an empty splash.
- **Logbook in the web.** The TUI has a completed-work-by-date Logbook; the web
  doesn't. Port it — it's the human-readable record of agent + human throughput.
- **Properties / frontmatter editing UI.** Structured tag/field editing
  (Obsidian "Properties"), writing back to YAML frontmatter (the
  frontmatter-preserving write path already exists).
- **Split panes / open-in-side.** Two notes side by side (Obsidian's daily
  workflow). Doable with htmx targets; significant CSS work.

### Tier 3 — Polish & reach

- **PWA / installable + offline service worker** for a real mobile experience
  (the responsive sidebar already exists).
- **Daily note / journal + calendar** view (Logseq-style capture).
- **Export** (print-to-PDF stylesheet, single-file HTML export).
- **Unlinked mentions** (Obsidian-style) in the backlinks panel.
- **Command palette → command runner**: not just "jump to note" but "run an
  action" (new note, toggle theme, complete task, go to ready queue).
- **Settings UI** (theme, width, edit-mode, default landing view).
- **Accessibility audit** (already has skip-link/aria — push to full keyboard
  operability and screen-reader passes).

---

## 5. Architecture guidance (staying true to the ADR)

- **Do not adopt an SPA framework.** ADR 0001 settled this; nothing here
  changes it. htmx + Alpine + server rendering reach every feature above with
  no `node_modules`, no build, offline, single binary. CodeMirror / a force-graph
  lib are **vendored single-file ESM bundles** (the mermaid precedent), not a
  toolchain.
- **Keep the hexagonal discipline.** Every write goes through `mutate`
  (lock + atomic + undo journal). The web must reuse it, never touch files
  directly — so CLI/TUI/MCP/web stay consistent and undoable.
- **Write path = `--edit`-gated + CSRF.** Tasks/notes/folder mutations extend
  the existing `allowEdit` + per-process CSRF model. Localhost-only is retained;
  no auth, no network bind.
- **The desktop story comes along for free.** Because Wails wraps the same
  `http.Handler` (`desktop/`), every Tier 1–2 feature lands in the desktop app
  automatically. When desktop graduates to a committed product (ADR Option D),
  the Tier 0 JSON API is the shared contract for a richer Go-bindings frontend —
  but that's additive, not a prerequisite.
- **Plain files stay the source of truth.** Any index (search, graph cache) is a
  rebuildable shadow derived from the files — the same principle the leading
  AI-memory tools converged on.

---

## 6. Suggested sequencing

1. **Tier 0** (htmx + JSON/fragment API + fsnotify-invalidated cache) — unblocks
   everything and fixes the one named maintainability smell.
2. **Interactive tasks + note/folder creation** (Tier 1) — converts the wedge
   (§3) from read-only to the actual memory GUI; highest identity payoff.
3. **Live Markdown editing** (split preview → CodeMirror autocomplete) — closes
   the most-felt PKM gap.
4. **Activity timeline + agent-memory home page** (Tier 2) — the
   category-defining differentiator.
5. **Force-directed graph + Logbook + search upgrade** — signature polish.
6. **Tier 3** opportunistically.

Each item is independently shippable, additive, and keeps `nt`'s
single-binary / no-build / offline guarantees intact.

---

## Sources

- [Obsidian vs Logseq vs Notion: PKM Systems Compared 2026](https://dasroot.net/posts/2026/03/obsidian-logseq-notion-pkm-systems-compared-2026/)
- [Obsidian vs Logseq (2026): Which Plain-Text PKM Wins?](https://www.atlasworkspace.ai/blog/obsidian-vs-logseq)
- [Basic Memory — AI Markdown knowledge base over MCP](https://mcpservers.org/servers/basicmachines-co/basic-memory)
- [sqlite-memory — Markdown-based AI agent memory](https://github.com/sqliteai/sqlite-memory)
- [memsearch — unified Markdown-backed agent memory layer](https://github.com/zilliztech/memsearch)
- [3D / force-directed graph view for Obsidian (plugin)](https://github.com/Apoo711/obsidian-3d-graph)
- [HTMX vs Alpine.js: when to use which](https://blog.openreplay.com/htmx-vs-alpine-when-use/)
- [Full-Stack Go app with HTMX and Alpine.js](https://ntorga.com/full-stack-go-app-with-htmx-and-alpinejs/)
- [Tiptap / Milkdown / CodeMirror editor comparison (OpenNotas)](https://docs.opennotas.io/started/editor/)
</content>
</invoke>
