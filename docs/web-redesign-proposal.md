# Web GUI redesign proposal — TypeScript SPA over a JSON API

- **Status:** Proposal for review (no code written). Supersedes the "no SPA" stance of [ADR 0001](adr/0001-web-frontend-and-desktop.md) **if accepted**.
- **Date:** 2026-06-08
- **Author:** External frontend architecture consult
- **Scope:** `internal/web` (the `nt web` GUI) and its build/release/desktop path
- **Decision driver (stated by client):** "future-proofing" — move the GUI to a modern TS framework, rebuilt afresh. Current implementation kept as `backup/web-v1-server-rendered-htmx`.

> **Read this first (§8 expanded into the TL;DR).** I was hired to design the rebuild, and this document designs it in full. But my job is also to tell you when a decision makes things worse, and this one does — measurably, against the *exact* properties your product page sells. The honest professional recommendation is in §8: **do not do a greenfield SPA rebuild now.** There is no concrete feature that the current htmx adapter cannot reach, and the rebuild trades away "one binary, no toolchain" — the single thing no competitor in your category has. If you read nothing else, read §8 and §6. The rest is the design you asked for, ready to execute the day a real trigger appears.

---

## 0. What `nt web` actually is today (independently verified)

I read the code, not the summary. The numbers in the brief are accurate:

| Layer | File | LOC | Notes |
|---|---|---|---|
| HTTP adapter | `internal/web/server.go` | 1,362 | routes, handlers, SSE hub, fsnotify watcher, tree builder |
| Markdown render | `internal/web/render.go` | 209 | goldmark + Chroma + wikilink rewrite; **single render path** reused by note view, preview, and (future) anything |
| Read-model | `internal/web/readmodel.go` | 219 | in-memory snapshot: notes + backlinks + taskRefs + fwd adjacency + activity + orphan set, rebuilt on change |
| Vanilla JS | `assets/app.js` + `assets/graph.js` | 394 + 184 = **578** | one IIFE each, progressive enhancement, zero npm deps |
| Styles | `assets/style.css` | 632 | hand-rolled Tokyo Night design system |
| Template | `assets/layout.html` | 348 | `html/template`, one file, all views + htmx fragments |
| Vendored | `htmx.min.js` (51 KB), `mermaid.min.js.gz` (904 KB gz) | — | embedded, **zero external requests** |

The architecturally decisive fact (which the rebuild must not break): **the web layer is one adapter in a hexagon.** All real logic — `task`, `note`, `links`, `search`, `store`, `mutate` — is shared with the CLI, TUI, and MCP server. Whatever we build on the frontend sits over the same domain.

### Seams that already exist (and that a JSON API formalizes)

The current code is *already* half-API. I verified these JSON/fragment seams in `server.go`:

- `GET /search?json=1&q=` → `[]{URL,Title,Path}` JSON (search-as-you-type, `server.go:715`).
- `GET /n/<h>?preview=1` → `{title,snippet}` JSON (hover popover, `server.go:473`).
- `GET /n/<h>?raw=1` → `text/plain` raw note + **`ETag` header** (editor load, `server.go:478`).
- `GET /?fragment=dash`, `/activity?fragment=1`, `/tasks?fragment=1` → HTML fragments for SSE-driven refresh.
- `GET /graph` page embeds `graphData` as `<script type="application/json">` (`server.go:986`, `buildGraphData`).
- `POST /preview` → renders an editor buffer to HTML via the *same* `renderBody` (`server.go:587`).
- `POST /n/<h>` save with **`If-Match` → 409** lost-update guard (`server.go:559`).
- `POST /tasks/...` task mutations, each through `mutate.Engine.Apply` (`server.go:833` `taskWrite`).
- `GET /events` typed SSE (`reload` | `tasks`), heartbeat every 25s, bfcache-safe close in `app.js:91`.

This matters enormously for the proposal: **the v1 author already paid down most of the API-design cost.** A rebuild is not designing a contract from nothing; it is hardening seams that exist into stable JSON. That makes the *incremental* path (§5) far cheaper than the greenfield path, and is a load-bearing reason for my §8 recommendation.

### Hard-won correctness in v1 that the rebuild MUST NOT regress

These were bugs that got fixed. Re-introducing them is the single biggest risk of "rebuilt afresh":

1. **Lost-update guard** (`server.go:559`): content-hash `ETag` (not mtime — comment at `server.go:148` explains why) captured on `?raw=1`, returned as `If-Match`, 409 on mismatch. The editor surfaces it (`app.js:371`).
2. **Debounced, self-write-aware watcher** (`server.go:1163` `watch`, `readmodel.go:194` `writeTracker`): 80 ms debounce coalesces the atomic-rename event burst; `selfWriteWindow` (750 ms) suppresses the adapter's own writes so a save doesn't bounce the editing client; `isTransient` ignores `.nt-*.tmp`/lock/undo/log.
3. **SSE connection lifecycle** (`app.js:78`): the EventSource is closed on `pagehide` and reopened on bfcache restore, because SSE pins an HTTP/1.1 socket and the browser caps ~6 per origin — without this, a few navigations exhaust the pool and every later load stalls. (Commit `807698b` literally fixed "progressive slowdown across page navigations.")
4. **Asset ETag caching** (`server.go:1045` `handleStatic`): content-hash ETag + `no-cache` so the browser 304s the ~900 KB mermaid bundle instead of re-downloading it per navigation.
5. **Read-model performance** (`readmodel.go`): backlinks/refs/adjacency precomputed once per change, not a per-request full-store ripgrep. This is the "snappy past 5k notes" work.

A greenfield SPA *changes the shape* of #2–#4 (one long-lived page instead of many navigations), which both removes some of these problems and creates new ones — discussed in §5.5.

---

## 1. Recommended stack

Opinionated, with the runner-up and the honest reason for rejection. Selection criteria, weighted for *this* project: (a) offline / fully self-hostable from `go:embed`, no CDN, no runtime fetch; (b) small bundle (you ship it inside a binary people `go install`); (c) excellent TS; (d) **maintainable by a Go-first team that does not live in JS**; (e) fits a Go JSON backend, not a JS meta-framework.

### 1.1 Framework — **Svelte 5 (runes)**

- **Why:** Smallest realistic runtime of the mainstream options (~2–3 KB CSR-only, ~5 KB with hydration), compiler-based so there is no virtual-DOM tax, fine-grained reactivity via runes (`$state`/`$derived`/`$effect`) that a Go dev reads as "signals," and a single-file-component model (`.svelte`) that keeps markup+logic+style co-located — close in spirit to the one-file `layout.html` you have now. Ecosystem ~4× larger than Solid by downloads, so hiring/answers are easier. First-class TS.
- **Runner-up: SolidJS.** Marginally faster fine-grained signals and the cleanest reactivity model in JS. **Rejected** because the ecosystem is smaller (more "you'll be writing it yourself" for a non-JS team), and the runtime/perf edge over Svelte 5 is imperceptible at nt's scale (hundreds–thousands of DOM nodes, not a trading terminal).
- **Also considered: Preact + signals.** ~3 KB, React-compatible mental model. **Rejected** as the *primary* pick because JSX-in-TS + the React idiom invites the team toward heavier React patterns over time; but it's the correct fallback if the team already knows React — see note below.
- **Explicitly rejected: React.** ~45 KB runtime before you add a router/query layer, and it pulls the team toward a heavier ecosystem. The only reason to pick React here is "we already know it." If that's true, **Preact + `@preact/signals` is the React-flavored answer** and I'd take it over React proper for bundle reasons.
- **Hard constraint check:** Svelte compiles to plain JS + CSS, no runtime CDN, trivially `go:embed`-able. ✅

### 1.2 Language — **TypeScript (strict)**

Non-negotiable and already the client's decision. `tsconfig` with `"strict": true`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax`. The payoff that matters most for a Go-first team: **generate TS types from Go** so the API contract is single-source-of-truth (see §4.4). No runner-up; plain JS is off the table by the engagement.

### 1.3 Build tool — **Vite 7** (Rolldown-based)

- **Why:** De-facto standard, instant HMR in dev, Rollup/Rolldown production builds with good tree-shaking and automatic code-splitting (critical: lets us lazy-load mermaid/CodeMirror so they're not in the initial bundle). First-class Svelte plugin. Outputs a hashed `dist/` that `go:embed` swallows whole.
- **Runner-up: esbuild alone.** Faster, simpler, already in spirit with Go's "fast tools." **Rejected** as the top-level driver because you lose Vite's dev server, HMR, framework plugin ecosystem, and manifest/code-splitting ergonomics — and Vite *uses* esbuild/Rolldown under the hood anyway, so you get its speed without hand-rolling the pipeline.
- **Constraint check:** dev needs Node; **production output is static and offline.** The Node dependency is a *build-time* cost only (this is the property being sacrificed — §6).

### 1.4 Router — **TanStack Router** (or SvelteKit's router if you go SvelteKit — see note)

- **Why:** Fully type-safe routes, search-param parsing/serialization (nt's `?source=`, `?tag=`, `?q=` filters become typed), and route-level loaders that integrate with the data layer. Code-splitting per route for free.
- **Runner-up: SvelteKit (its built-in router + file-based routing).** **Rejected as the default** because SvelteKit is a *meta-framework* oriented toward SSR/edge deploys and an adapter model — it fights "static SPA embedded in a Go binary." You'd run it as a static adapter (`@sveltejs/adapter-static`), which works, but you inherit SvelteKit's conventions and server concepts you don't want when Go *is* the server. **However**, if the team values batteries-included conventions over à-la-carte, SvelteKit-with-`adapter-static` is a legitimate alternative; it just makes the Go/JS boundary blurrier. My pick is Svelte (the library) + TanStack Router + Vite — explicit boundary, Go owns the server, JS owns the client.
- **Honest caveat:** TanStack Router's Svelte support trails its React support. If that gap bites at build time, fall back to SvelteKit static. This is the one place the Svelte choice has friction; budget a spike to confirm (§7).

### 1.5 Data / server-state — **TanStack Query** (`@tanstack/svelte-query`)

- **Why:** This is the single highest-value library in the stack for *this* app. nt's GUI is a read-heavy view over a store that **changes underneath you** (agents, CLI, other tabs, fsnotify). TanStack Query gives you caching, background refetch, stale-while-revalidate, request dedup, and — crucially — **`queryClient.invalidateQueries()` driven by the SSE event** so a `tasks` event surgically refreshes exactly the affected queries instead of reloading the page. It also gives optimistic updates with rollback, which is the right primitive for the 409 lost-update flow (§3.4).
- **Local UI state:** Svelte runes (`$state`) for ephemeral UI (palette open, editor buffer, graph viewport). No Redux/Zustand — overkill; server state lives in Query, UI state in component signals.
- **Runner-up: hand-rolled fetch + signals.** **Rejected:** you'd re-implement cache invalidation, dedup, and retry — and get the SSE-invalidation wiring subtly wrong, which is exactly the class of bug (#3 above) v1 already paid to fix.

### 1.6 Styling — **keep the hand-rolled CSS, ported to plain CSS files / `<style>` in components**

- **Why:** You already have a 632-LOC, coherent Tokyo Night design system with light/dark, reading-width, and theme-toggle baked in. **Porting it verbatim is cheaper and lower-risk than re-expressing it in Tailwind**, and it keeps the Chroma highlight CSS contract (`render.go:97` `highlightCSS`, theme-scoped via `[data-theme]`) intact. Svelte's scoped `<style>` blocks per component + a small global `app.css` for tokens/themes is the natural home.
- **Runner-up: Tailwind v4.** Genuinely good in 2026 (JIT, ~10–30 KB output, IntelliSense). **Rejected** because (a) you'd be *rewriting* a working design system to gain nothing users see, (b) it adds a PostCSS/Tailwind build step and a utility vocabulary the Go-first team must learn, and (c) your CSS is not the maintenance problem — the JS string-templating was, and the framework fixes that regardless of styling approach. If you were greenfield with *no* design system, Tailwind v4 would be my pick.
- **Rejected: vanilla-extract / CSS-in-JS.** Type-safe but solves a problem you don't have; more build machinery.

### 1.7 Markdown editor — **CodeMirror 6** (lazy-loaded), starting from the existing split-preview

- **Why CM6:** It is a *source* editor (not a WYSIWYG document model), which is the correct fit because **nt's source of truth is the raw `.md` file** — including frontmatter, todo.txt-style task links, and `[[wikilinks]]` the server resolves. CM6 gives syntax highlighting, and an extension API for inline `[[ ]]` autocomplete (fed by the note index you already ship), `#tag`/`+project` autocomplete (fed by `/tags`), and `⌘S` save. The current `app.js` editor is already a raw textarea + server-rendered split preview (`app.js:306`); CM6 is the incremental upgrade of the *left pane only* — the preview keeps calling `POST /preview` so rendering stays byte-identical to a save.
- **Runner-up: TipTap.** Best-in-class WYSIWYG. **Rejected** — and this is a strong reject — because TipTap/ProseMirror impose a *document model* that is not your Markdown file. Round-tripping arbitrary Markdown (frontmatter, wikilinks, mermaid fences, todo.txt tokens) through ProseMirror and back **will mangle files an agent or `$EDITOR` also writes.** That violates "plain files are the source of truth, any tool reads/writes them" (SPEC §1). Same reason to reject **Milkdown** (Crepe) despite its nice Markdown-first framing — it's still a ProseMirror doc model, and its bundle is heavy (the project itself has open issues about Crepe bundle size).
- **Honest cost:** CM6 is **not a single vendored file like mermaid** — it's many `@codemirror/*` + `@lezer/*` packages. Vite bundles them into one lazy-loaded chunk (~250–400 KB raw, code-split so it's *not* in the initial load). This is the editor-specific instance of the no-build sacrifice (§6). Gate the autocomplete work behind whether users actually demand it; the split-preview alone (which you already have) is a fine v1 of the editor.
- **Phasing:** keep the existing server-rendered split-preview as the editor on day one; add CM6 to the left pane in a later phase. Do **not** block the rebuild on CM6.

### 1.8 Markdown *rendering* — **keep rendering on the server (goldmark), don't add a client renderer**

This is a deliberate, important choice and a place the rebuild should resist the SPA reflex.

- **Why server-side:** `render.go` is the *single render path* — goldmark + GFM + Chroma highlighting + `[[wikilink]]` resolution against the live note graph + mermaid-fence rewriting. Wikilink resolution **requires the whole-store link index** (`links.Resolve` over notes/doc); you cannot faithfully reproduce that in the browser without shipping the index and re-implementing resolution in TS — a guaranteed source of divergence bugs. So the client fetches **rendered HTML** for note bodies (a `GET /api/notes/{h}` returning `{html, meta, backlinks, ...}`), and only renders chrome itself.
- **Consequence:** the SPA's per-note view is "fetch JSON → set `innerHTML` of the `.md` container → run mermaid + reading-enhancements." Mermaid stays a lazy-loaded client bundle (it must — it's a runtime layout engine), gated to pages that contain a `.mermaid` block.
- **Runner-up: client-side `marked` (~12 KB) or `markdown-it` (~45 KB).** **Rejected** for note bodies — they can't resolve wikilinks or match Chroma output, so the GUI would render notes differently from every other adapter. (A client renderer is acceptable *only* for trivial, link-free text like preview snippets, where it isn't worth a round-trip.)

### 1.9 Graph visualization — **keep the existing zero-dep canvas force layout; upgrade to a vendored WebGL lib only if 5k+ nodes demands it**

- **Why:** `graph.js` (184 LOC) already does pan/zoom/drag, hover-neighborhood highlight, click-to-open, filter, and color-by-folder/source on a 2D canvas with a hand-rolled Verlet sim. It is *good*, zero-dep, and offline. Port it to a TS module as-is.
- **Upgrade path if it stutters past a few thousand nodes:** `cosmograph`/Cosmos (GPU/WebGL force sim, handles ~1M nodes) or PixiJS + d3-force. **Rejected as the default** because they're heavier deps and your canvas implementation already meets the bar for realistic stores; adopt only against a measured frame-rate problem on a real 5k-note graph.
- **Runner-up considered: `vasturiano/force-graph` (Canvas) / `3d-force-graph` (Three.js).** Nice, but more bundle for capability you already have in 184 LOC.

### 1.10 Diagrams — **Mermaid, lazy-loaded, vendored (unchanged policy)**

Keep mermaid; it's the only place a runtime diagram engine is warranted. The win available in the rebuild: **code-split it** so it loads only on note pages that contain a `mermaid` fence, instead of being a global script. (Today it's a global `<script>` mitigated by ETag 304s.) Keep it vendored/bundled — no CDN.

### 1.11 Testing — **Vitest (unit/component) + Playwright (e2e)**

- **Vitest:** native Vite integration, Jest-compatible API, fast; component tests via `@testing-library/svelte`. Unit-test the pure stuff: API client, optimistic-update reducers, the 409-conflict state machine, search-param parsing.
- **Playwright:** the e2e harness that must **prove the v1 correctness properties did not regress** (§5.5) — drive a real `nt web --edit` against a temp store and assert: 409 on concurrent edit, SSE refresh on external CLI write, no socket exhaustion across N navigations, asset 304s. This is the regression net for the bugs v1 fixed.
- **Runner-up: Cypress.** **Rejected:** Playwright is faster, multi-browser, better CI story, and better at the SSE/network assertions we specifically need.
- **Go side:** keep `web_test.go`; add Go-level tests for the new JSON endpoints (contract tests) so the API is verified without a browser.

### Stack summary

| Concern | Pick | Runner-up (rejected because) |
|---|---|---|
| Framework | **Svelte 5** | Solid (smaller ecosystem); Preact (React idiom creep) |
| Language | **TypeScript strict** | — |
| Build | **Vite 7** | esbuild alone (no dev server/HMR/splitting) |
| Router | **TanStack Router** | SvelteKit static (meta-framework blurs Go/JS line) |
| Server-state | **TanStack Query** | hand-rolled (re-implements cache + SSE invalidation) |
| UI state | **Svelte runes** | Zustand/Redux (overkill) |
| Styling | **port existing CSS** (scoped + tokens) | Tailwind v4 (rewrite for no user-visible gain) |
| MD editor | **CM6** (lazy, phased) | TipTap/Milkdown (ProseMirror mangles plain files) |
| MD render | **server goldmark (unchanged)** | marked/markdown-it (can't resolve wikilinks/match Chroma) |
| Graph | **existing canvas → port** | cosmograph/force-graph (only if 5k+ stutters) |
| Diagrams | **mermaid lazy + vendored** | — |
| Unit/e2e | **Vitest + Playwright** | Cypress (slower, weaker net assertions) |

---

## 2. Backend / API redesign

The server stops rendering pages and becomes a JSON API (plus one HTML-fragment endpoint kept on purpose). It still serves the embedded SPA shell at `/`. **Every write still goes through `mutate.Engine.Apply`** (`server.go:843`); none of the domain changes.

### 2.1 Principles

- **Read model unchanged.** The JSON handlers project from the existing `snapshot` (`readmodel.go`). The expensive precompute (backlinks, refs, adjacency, activity) is reused, not rebuilt.
- **Server still renders note *body* HTML.** Per §1.8, the API returns rendered HTML for note bodies so wikilink resolution + Chroma highlighting stay single-source. Everything else is data.
- **Stable handles.** Notes addressed by `noteHandle` (ULID, else rel path) — unchanged from `server.go:1357`.

### 2.2 Endpoints (concrete)

All under `/api`, all JSON unless noted. `GET` is always allowed; mutations require `--edit` + CSRF (§2.5).

```
GET  /api/state                      → { canEdit, version, openCount, noteCount, sources[], builtAt }
GET  /api/notes                      → tree + flat index:
                                       { tree: TreeNode[], index: {id,title,path}[] }
GET  /api/notes/{handle}             → { id, title, folder, file, crumbs[], source, created,
                                         tags[], bodyHTML, backlinks[], taskRefs[],
                                         prev?, next?, etag }       # bodyHTML from renderBody
GET  /api/notes/{handle}/raw         → { text, etag }              # CM6/editor load (was ?raw=1)
POST /api/notes/{handle}             → save; body {text}; header If-Match:<etag>; 204 | 409
POST /api/notes                      → create note; body {title, folder?}; → {handle}   # NEW (Tier 1 gap)
POST /api/preview                    → body {text} → { html }      # was POST /preview
GET  /api/search?q=&tag=&source=&kind=&limit=&cursor=
                                     → { results: Hit[], nextCursor? }   # ranked, paginated
GET  /api/tasks?status=&source=      → { groups: [{status, tasks: Task[]}] }
POST /api/tasks                      → add;    body {text,pri?,due?,project?} → Task
POST /api/tasks/{id}/done            → Task (or {ok})
POST /api/tasks/{id}/reopen          → Task
POST /api/tasks/{id}/status          → body {status} → Task
DELETE /api/tasks/{id}               → {ok}
GET  /api/activity?source=&cursor=&limit=   → { days: [{date, events: Event[]}], nextCursor? }
GET  /api/graph?folder=&source=      → { nodes: GraphNode[], links: GraphLink[] }   # same shape as buildGraphData
GET  /api/preview/{handle}           → { title, snippet }          # hover popover (was ?preview=1)
GET  /events                         → SSE, unchanged transport (see §2.6)
GET  /                               → SPA index.html (embedded)
GET  /assets/*                       → hashed SPA assets (embedded, immutable cache)
```

Payload shapes mirror the existing structs so the projection is mechanical:
- `Task` = `{id, text, status, due, source, project, tags[], blocker?}` (today's `taskRow`).
- `Event` = `{when, action, kind, source, title, url?}` (today's `activityEvent`).
- `Hit` = `{url, title, path, snippet?, kind}` (today's `linkRow` + ranking).
- `GraphNode/Link` = exactly `buildGraphData`'s output (`server.go:964`).

### 2.3 What changes vs today

- HTML page handlers (`handleIndex`, `handleNote` page branch, `handleTags`, etc.) are **deleted**; their data projections become JSON handlers reusing the same `snapshot` accessors.
- `render` / `pageData` / template execution goes away (templates move to the client). **`renderBody` stays** — it's the value, and it's now called by `/api/notes/{h}` and `/api/preview`.
- New: `POST /api/notes` (note creation) — closes the real Tier-1 gap (today you must drop to the terminal; `note.Create` exists in the domain).

### 2.4 Writes → `mutate` (unchanged contract)

Task writes keep the exact `taskWrite` pattern (`server.go:833`): `--edit` gate → CSRF check → `writes.mark(tasksFile)` (self-write suppression) → `eng.Apply(op, fn)` (lock + re-read + undo journal) → `rebuild()` → `hub.broadcast("tasks")`. The only difference is the handler returns **JSON** (the updated task / list) instead of an HTML fragment.

Note saves keep `handleSave` (`server.go:540`): `--edit` + CSRF + `If-Match` ETag check + 4 MiB cap + trailing-newline normalize + `WriteAtomic` + `rebuild`. **Open item carried from v1:** note saves still bypass the undo journal (they call `store.WriteAtomic` directly, not `eng.Apply`). The rebuild is a good moment to route note writes through a journaled path too — but that's a *domain* change, independent of the frontend, and should be its own task.

### 2.5 CSRF + `--edit` gating (unchanged model, JSON-framed)

- Read-only by default; `nt web --edit` flips `allowEdit` (`server.go:196`).
- Per-process random CSRF token (`server.go:51`, `randToken`). The SPA reads it from `GET /api/state` (instead of the `<meta name="csrf">` tag at `layout.html:9`) and sends it as `X-CSRF` on every mutation. As today, requiring a **custom header forces a CORS preflight a cross-site page can't satisfy** — that's the whole CSRF defense, and it's preserved verbatim.
- Localhost-only bind (`127.0.0.1`), no auth, no network exposure — unchanged (SPEC §12.1).
- Mutation handlers return **403** when `!allowEdit` or bad CSRF (as today). The SPA hides edit affordances when `state.canEdit === false` and treats 403 as "edit mode off."

### 2.6 Live updates — **keep SSE, do not switch to WebSocket**

- **Why SSE:** the live-update need is strictly *server → client* notifications ("something changed; here's the kind"). SSE is the right tool: one-directional, auto-reconnect built into `EventSource`, trivial over plain HTTP, and **already correct in v1** (typed events, heartbeat, bfcache lifecycle). WebSocket buys bidirectional messaging you don't need and costs you a heartbeat/reconnect/backpressure protocol you'd have to write.
- **The SPA actually makes SSE *better*:** today every navigation reopens the EventSource (the source of the socket-exhaustion bug `app.js` had to defend against). In a single-page app there is **one** EventSource for the app's lifetime — the bfcache/`pagehide` dance becomes a non-issue, and the typed event maps cleanly to `queryClient.invalidateQueries(['tasks'])` etc. This is one genuine architectural win of the rebuild (§5.5).
- **Keep the typed payload** (`reload` | `tasks` | future `notes`). Wire each kind to specific query invalidations; `reload` becomes "invalidate everything" rather than `location.reload()`.
- **WebSocket runner-up: rejected** — bidirectional capability unused, more moving parts, and it would re-introduce a reconnect lifecycle SSE gives free.

### 2.7 Pagination / performance for large stores (5k+ notes)

The read-model already scales (precomputed maps). The new constraints are *payload size* over the wire and *DOM size* in the SPA:

- **Notes tree/index** (`GET /api/notes`): a flat index of 5k notes is ~a few hundred KB JSON — fine once, cached by Query, reused by palette/search/graph. Tree is the same data nested. No pagination needed for the index; **virtualize the rendered tree** in the client (render only visible rows) rather than paginating the data.
- **Search** (`GET /api/search`): add `limit` + `cursor` and **server-side ranking with highlighted snippets** (a real Tier-1 gap today — `handleSearch` does substring + ripgrep with no ranking). Cap default to ~50 hits.
- **Activity** (`GET /api/activity`): cursor-paginate by time; default last ~50 events (today `buildDashboard` slices `[:8]`, `groupActivity` returns all — unbounded). The full timeline should page.
- **Graph** (`GET /api/graph`): for very large graphs, support `?folder=`/`?source=` server-side filtering and a "local graph" mode (neighbors of a focus node) so the client isn't forced to lay out 5k nodes at once. The WebGL upgrade (§1.9) is the other half of this if it's ever needed.
- **Note body**: rendered HTML per note is fetched on demand and cached by Query — never ship all bodies.

---

## 3. Frontend architecture

### 3.1 Routes / pages

| Route | Page | Data source |
|---|---|---|
| `/` | **Dashboard** (agent-memory home: ready / doing / blocked+why / recent activity / recently-viewed) | `/api/tasks`, `/api/activity`, localStorage |
| `/n/:handle` | **Note view** (body HTML, meta, tags, backlinks, taskRefs, prev/next, TOC, mermaid) | `/api/notes/:handle` |
| `/n/:handle?edit` | **Editor** (split: CM6/textarea left, live preview right) | `/api/notes/:handle/raw` + `/api/preview` |
| `/tasks` | **Tasks** (grouped by status, interactive: done/reopen/status/delete/add) | `/api/tasks` |
| `/graph` | **Graph** (force canvas; filter; color by folder/source; local-graph mode) | `/api/graph` |
| `/activity` | **Activity** (provenance timeline, source filter, paginated) | `/api/activity` |
| `/search?q=…` | **Search results** (ranked, highlighted, filter by tag/folder/source/kind) | `/api/search` |
| `/tags`, `/orphans` | tag cloud / orphan finder | `/api/notes` projections |
| `⌘K` (overlay, not a route) | **Command palette** (jump-to-note; later: command runner) | flat index from `/api/notes` |

Persistent shell: sidebar (search box, section nav, folder tree), topbar (breadcrumbs, edit/width/theme toggles), palette overlay — all the chrome that's in `layout.html` today, now persistent across navigations (the SPA win).

### 3.2 Component breakdown (Svelte)

```
App
├─ Shell (Sidebar, Topbar, PaletteOverlay, SSEProvider)
│   ├─ Sidebar: SearchBox, SectionNav, NoteTree(virtualized)
│   └─ Topbar: Breadcrumbs, EditToggle, WidthToggle, ThemeToggle
├─ routes/
│   ├─ Dashboard (DashStats, ReadyList, DoingList, BlockedList, ActivityFeed, RecentlyViewed)
│   ├─ NoteView (NoteHeader, NoteBody[innerHTML+mermaid+enhancers], TaskRefs, Backlinks, Pager, Toc)
│   ├─ NoteEditor (EditorPane[CM6 lazy], PreviewPane, EditBar[save/cancel/status])
│   ├─ Tasks (TaskAddForm, TaskGroup, TaskRow[check/status/delete])
│   ├─ Graph (GraphCanvas[ported graph.js], GraphControls)
│   ├─ Activity (ActivityDay, ActivityRow, SourceFilter)
│   └─ Search (SearchFilters, ResultList, ResultRow[highlighted])
└─ lib/
    ├─ api.ts (typed client, generated types — §4.4)
    ├─ queries.ts (TanStack Query hooks: useNote, useTasks, useActivity, useGraph, useSearch)
    ├─ sse.ts (single EventSource → query invalidations)
    ├─ csrf.ts, theme.ts, recent.ts (localStorage)
    └─ mermaid.ts, enhancers.ts (TOC/anchors/copy-buttons — ported from app.js:95)
```

The progressive-enhancement logic in `app.js` (heading anchors, TOC, scrollspy, copy buttons, recently-viewed, folder-collapse persistence, theme/width toggles, hover previews) ports almost line-for-line into `enhancers.ts` + small components — it's the same behavior, just attached after `bodyHTML` is set rather than on full-page load.

### 3.3 State model

- **Server state → TanStack Query.** One query per resource (`['note', handle]`, `['tasks']`, `['activity', filters]`, `['graph', filters]`, `['search', q, filters]`, `['notes']`). `staleTime` modest; SSE drives invalidation so polling isn't needed.
- **UI state → runes.** `$state` for: palette open, editor buffer + dirty flag, graph viewport, sidebar-open (mobile), theme/width (mirrored to localStorage).
- **Derived → `$derived`.** e.g. filtered task groups, palette matches.

### 3.4 Optimistic updates + 409 handling (the careful bit)

- **Tasks (optimistic):** check/status/delete apply optimistically via Query's `onMutate` (snapshot → patch cache → render instantly), with rollback on error. The server response (real `Task`) reconciles. This is strictly nicer than today's htmx round-trip-then-swap. Low risk because task writes are journaled and idempotent-ish.
- **Notes (pessimistic + explicit 409):** **do not** optimistically overwrite note saves. Reproduce v1's contract exactly: editor holds the `etag` from `/api/notes/:handle/raw`; `POST` sends `If-Match`; on **204** reload the rendered note; on **409** *do not discard the buffer* — surface "changed on disk — reload to merge," offer a diff/reload, and let the user decide. This is the `app.js:371` behavior, preserved. **This must have a Playwright test** (§5.5) because it's the highest-value correctness property and the easiest to silently break in a rewrite.
- **SSE reconciliation:** a `tasks` event invalidates `['tasks']` and `['activity']`; if the user is *editing* a note and a `reload`/`notes` event names that note, show a non-destructive "this note changed underneath you" banner rather than clobbering the buffer.

### 3.5 Offline behavior / PWA stance

**Recommendation: do NOT ship a service-worker PWA in the rebuild.** Reasoning:
- The app is served from `127.0.0.1` by a binary the user is *already running*; "offline" is automatic — there's no network to lose. A service worker adds cache-invalidation complexity (the classic "users stuck on a stale SW" failure) for a benefit that only matters if you wanted to *install it as a standalone app away from the server*, which contradicts "localhost-only, the binary is the server."
- It's a Tier-3 "reach" item in the existing roadmap, not a driver. If a genuine mobile-away-from-desktop use case appears, revisit — but it's out of scope for "future-proofing the GUI."
- The responsive sidebar already gives a decent phone experience when pointed at the running server on the LAN (which you'd have to *choose* to expose; default stays localhost).

So: offline is satisfied structurally (everything embedded, no CDN, no runtime fetch beyond your own localhost API). No SW.

---

## 4. Project structure + build / CI / embed

### 4.1 Where the frontend lives

```
internal/web/
├─ server.go            # JSON API + SPA serving + SSE + watcher (slimmed: no templates/render path for pages)
├─ render.go            # KEEP — renderBody, the single MD render path (now called by JSON handlers)
├─ readmodel.go         # KEEP — snapshot (now projected to JSON)
├─ api.go               # NEW — JSON handlers
├─ embed.go             # //go:embed frontend/dist  → serves the built SPA
├─ web_test.go          # + contract tests for JSON endpoints
└─ frontend/            # NEW — the Vite/Svelte app
   ├─ package.json, vite.config.ts, tsconfig.json
   ├─ src/ (App, routes/, lib/, components/, app.css)
   ├─ index.html
   └─ dist/             # build output, GIT-IGNORED, embedded at Go build time
```

Keep the frontend **inside** `internal/web/` so `go:embed frontend/dist` is a relative embed and the adapter stays self-contained. `dist/` is git-ignored and generated; the Go build embeds whatever is there.

### 4.2 Embedding (the single-binary constraint, preserved)

```go
//go:embed all:frontend/dist
var distFS embed.FS
```
Serve `dist/index.html` at `/` and `dist/assets/*` (hashed, `Cache-Control: immutable`) at `/assets/`. SPA fallback: any non-`/api`, non-`/events`, non-asset path returns `index.html` so the client router handles it. **`go build` still yields one static binary** — the constraint holds, *provided `dist/` was built first*. That "provided" is the whole sacrifice (§6).

### 4.3 Dev workflow

- **Dev:** `vite dev` on :5173 (HMR) + `nt web --edit` on its own port. Vite `server.proxy` forwards `/api`, `/events`, `/static` → the Go server. You edit Svelte with hot reload while hitting the real domain/store. (This is the well-trodden Vite-proxy-to-Go pattern.)
- **Prod build:** `npm ci && npm run build` → `frontend/dist` → `go build` embeds it.
- **Make targets:** `make web-dev` (vite + go), `make web-build` (npm build), and `make build` gains a dependency on `web-build`. A `//go:generate` directive can document the step, but CI should call it explicitly (don't rely on `go generate` in release).

### 4.4 Type generation (high leverage for a Go-first team)

Generate TS types from Go so the contract can't drift. Options: `tygo` (Go structs → TS interfaces) or an OpenAPI spec → `openapi-typescript`. **Recommend `tygo`** — minimal, no runtime, annotate the API response structs and emit `frontend/src/lib/api-types.ts` in CI. This is the antidote to the #1 long-term risk of a split stack (Go and TS disagreeing about shapes) and makes the boundary cheap for people who think in Go.

### 4.5 CI changes

`ci.yml` today is pure Go (gofmt, vet, test). Add a frontend job:
```yaml
web:
  - setup-node (pin version via .nvmrc / package.json "engines")
  - npm ci            # lockfile-frozen install (supply-chain: commit package-lock.json)
  - npm run lint      # eslint + svelte-check + tsc --noEmit
  - npm run test      # vitest
  - npm run build     # must succeed; produces dist/
  - npx playwright test   # e2e against a built `nt web --edit` (the regression net, §5.5)
  - tygo / type-check that generated types match
```
And the Go test job must build the frontend first (or commit a checked-in `dist/` for `go test` — see §4.6 trade-off). Add `npm audit`/Dependabot for the supply-chain surface you're newly taking on.

### 4.6 `go install` / curl installer / GoReleaser impact — **the sharp edge**

This is where the sacrifice bites hardest and must be called out plainly:

- **`go install github.com/navbytes/nt@latest` breaks** unless `frontend/dist` is **committed to the repo**, because `go install` runs `go build` with *no Node, no npm, no Vite*. Today `go install` works because the assets are hand-written files already in the tree. After the rebuild, the embedded assets are *build outputs*.
  - **Mitigation A (recommended): commit `dist/`.** Check the built bundle into git (or a generated-artifacts dir). Restores `go install`, keeps the curl/Homebrew/binary stories identical. **Cost:** a build artifact in version control (noisy diffs, must be regenerated on every frontend change, CI must verify it's up to date — a `git diff --exit-code` after `npm run build`). This is the standard answer for "embed a JS build in a Go module that people `go install`."
  - **Mitigation B: drop `go install` support** and tell source users to run `make build`. **Cost:** loses a real install channel and a selling point.
  - I recommend **A**, with a CI check that the committed `dist/` matches a fresh build, so it can't silently rot.
- **GoReleaser (`.goreleaser.yaml`):** add a `before.hooks` step `npm ci && npm run build` (and Node in the release runner) so release binaries embed a fresh bundle — regardless of whether `dist/` is committed. The build matrix, `CGO_ENABLED=0`, archives, checksums all stay the same. The CLI is still one static binary per platform.
- **curl installer:** unaffected — it downloads a prebuilt binary; the binary still has everything embedded.
- **Desktop (Wails):** see §4.7.

### 4.7 Desktop (Wails) path

Two viable approaches; pick based on how committed desktop becomes (still a spike per ADR 0001 / `desktop/README.md`):

- **Keep "wrap the `http.Handler`" (recommended for now).** The desktop module already passes `web.Server.Handler()` to `assetserver.Options{Handler:...}`. After the rebuild, that same handler serves the **embedded SPA + JSON API** — so the native WebKit window renders the new SPA *unchanged*, exactly as it renders the htmx UI today. **Zero desktop-specific frontend work**, and the CLI's single-binary purity is still isolated in the nested module. This is the cheapest path and keeps ADR 0001's "desktop comes free" property.
- **Future (ADR Option D): Wails Go-bindings.** Bind Go methods directly to the SPA instead of going over localhost HTTP. More native, but it's an *additive* evolution to do when desktop is a committed product — not required by this rebuild. The JSON API is the shared contract either way.

Net: the rebuild **does not** force any desktop change. The handler-wrap approach carries the new SPA into the native window for free.

---

## 5. Migration & coexistence

### 5.1 Greenfield vs incremental — **strong recommendation: incremental islands, not a big-bang greenfield**

The brief says "rebuilt afresh," and I'll design that — but the lowest-risk route to the same end state exploits the seams that already exist (§0): **mount Svelte islands into the existing `layout.html` shell behind the JSON seams, one view at a time**, then retire the server-rendered shell last. Reasons:

- The JSON/fragment seams already exist; you can stand up `/api/tasks` and replace *only* the tasks view with a Svelte island while the rest stays server-rendered. Each step is shippable and reversible.
- It keeps the v1 correctness machinery live and exercised during the transition, so regressions show up immediately against a working reference — instead of discovering them all at cutover.
- It de-risks the two Svelte-specific unknowns (TanStack Router on Svelte; CM6 bundling) before they're load-bearing.

If the team insists on greenfield, the design above supports it; the phasing in §7 just collapses into "build all routes, then cut over."

### 5.2 How the backup coexists

- `backup/web-v1-server-rendered-htmx` is the known-good reference. Keep `nt web` pointing at v1 on `main` until the SPA reaches parity behind a flag.
- **Coexistence flag:** `nt web --ui=spa` (or `NT_WEB_SPA=1`) serves the new SPA; default stays v1 until cutover. Both compile into the same binary during transition (small cost: both asset sets embedded), so you can A/B in one build. Drop the flag and the old assets at cutover.

### 5.3 Cutover plan

1. Land JSON API alongside the existing HTML handlers (additive; v1 untouched).
2. Build SPA route-by-route behind `--ui=spa`; verify each against v1.
3. Playwright parity + regression suite green (§5.5).
4. Flip default to SPA; keep `--ui=v1` as escape hatch for one release.
5. Remove v1 templates/`app.js`/`graph.js`/htmx, the HTML page handlers, and the `--ui` flag. `backup/` branch remains the historical record.

### 5.4 Explicitly avoid re-introducing v1's fixed bugs

A checklist the rebuild must satisfy (each maps to §0's hard-won list and gets a test in §5.5):

- **Lost-update guard:** ETag captured on raw load, `If-Match` on save, 409 surfaced non-destructively. Keep `handleSave` server-side logic *as is*; the client must send `If-Match` and handle 409 (§3.4).
- **Debounced/self-write-aware watcher:** **do not touch `watch`/`writeTracker`/`isTransient`** — they're server-side and correct. Keep `writes.mark` on every write path.
- **SSE lifecycle:** one EventSource for the SPA lifetime (the rebuild makes this *easier*, §2.6); still handle reconnect (EventSource does it) and don't leak it on hot-reload in dev.
- **Asset caching:** hashed filenames + `immutable` for SPA assets is *better* than v1's ETag/no-cache dance — but keep ETag/no-cache for the (now smaller) set of non-hashed responses. Don't regress the mermaid 304 behavior (now solved by code-splitting + content-hash filenames).
- **Read-model perf:** JSON handlers project from `snapshot`; never add a per-request full-store walk. Keep the warm-on-start + rebuild-on-change lifecycle.

### 5.5 Regression net (Playwright + Go contract tests)

Concrete tests that must exist before cutover:
1. **409 path:** open editor (capture etag) → external `nt edit`/`WriteAtomic` to same note → save → assert 409 and buffer preserved.
2. **SSE external write:** open tasks view → `nt add` via CLI against the store → assert the list refreshes without manual reload.
3. **No socket exhaustion:** navigate 20×; assert no stalled requests / one EventSource.
4. **Asset 304 / cache:** reload; assert hashed assets are cache-hits, not refetched.
5. **CSRF/edit gate:** mutation without `X-CSRF` → 403; with `--edit` off → 403; affordances hidden.
6. **Read-model under load:** seed 5k notes; assert note/tasks/search responses stay snappy and no per-request ripgrep (Go-level).

---

## 6. Risks & trade-offs — what gets WORSE (candidly)

| What gets worse | Detail | Mitigation |
|---|---|---|
| **No-build property — gone** | This is the headline loss. Today a contributor edits `app.js`/`layout.html` and rebuilds the Go binary. After: they need Node, npm, a lockfile, `npm ci`, `vite build`. The "edit a file, `go build`" loop is dead for the frontend. | Commit `dist/` so *Go-only* contributors and `go install` still work (§4.6); document the Node toolchain; pin versions. You cannot mitigate the loss for *frontend* contributors — it's inherent. |
| **Single-binary purity — asterisked** | The binary is still single and static, but it's now a *build artifact of a build artifact*. Reproducibility now depends on a Node toolchain + npm registry. | GoReleaser `before` hook builds the bundle; `CGO_ENABLED=0` unchanged; commit-and-verify `dist/`. |
| **`go install` fragility** | Breaks unless `dist/` is committed (§4.6). A whole install channel and a documented value-prop at risk. | Commit `dist/` + CI freshness check. Real residual cost: artifact in VCS. |
| **Contributor friction** | Two languages, two toolchains, two test runners, two linters. A Go-first maintainer team now must keep a TS/Svelte app healthy (deps age fast in JS-land — frameworks, Vite majors, transitive CVEs). | tygo type-gen to keep the boundary mechanical; Dependabot; keep the dep list *small* (the stack above is deliberately lean). |
| **Bundle size — up** | v1 ships 578 LOC JS + 51 KB htmx + (lazy) mermaid. SPA initial bundle = Svelte (~5 KB) + router + Query + app code ≈ **80–150 KB gz** before mermaid/CM6 (both code-split). Still small, but strictly more than htmx-fragments. | Code-split mermaid + CM6 (not in initial load); tree-shake; the absolute numbers stay modest. |
| **Supply-chain surface — new** | You now depend on the npm graph (hundreds of transitive packages) and its CVE stream. v1 had *zero* npm deps. | Lockfile committed, `npm ci` only, `npm audit`/Dependabot, minimal deps, vendored/bundled (no runtime CDN — offline preserved). |
| **Offline complexity** | Offline is *preserved* (no CDN, everything embedded), but proving it requires discipline — a stray `import` from a CDN or a font fetch would break the guarantee silently. | Build-time check: no external URLs in `dist/`; Playwright run with network blocked. |
| **The category selling point — eroded** | Your strongest differentiator in the AI-memory category (per `web-industry-leading.md`) is **"one binary, no toolchain, fully offline"** while competitors say "just open it in Obsidian." A npm build step makes you *more* like everyone else on the dimension you were uniquely winning. | This one you largely *eat*. The mitigation is honesty: keep the *runtime* story (single offline binary) even as the *build* story converges with the field. |
| **Complexity for a thin adapter** | You're adding an SPA toolchain to replace ~1,400 LOC of glue that works. The framework earns its keep only past a complexity threshold the app hasn't clearly hit. | This is the §8 argument. |

What gets **better** (to be fair): persistent shell (no per-nav reload), one SSE for the app's life (kills the socket-exhaustion class), optimistic task updates, typed end-to-end contract, easier rich-editor/autocomplete, code-split heavy deps, and a genuinely nicer DX *for people who already do frontend*.

---

## 7. Effort estimate + phasing

Assumes one engineer comfortable in both Go and TS/Svelte. Ranges, not promises.

| Phase | Work | Effort |
|---|---|---|
| **0. Spikes (de-risk)** | TanStack Router-on-Svelte viability; CM6 bundling under Vite; tygo type-gen; `go install` with committed `dist/`. **Go/no-go gate.** | 3–5 days |
| **1. JSON API** | Add `/api/*` handlers projecting from `snapshot`; keep `renderBody`; contract tests; tygo types. v1 untouched. | 1–1.5 weeks |
| **2. App skeleton** | Vite+Svelte+Router+Query scaffold; shell (sidebar/topbar/palette); theme/width/CSS port; SSE→invalidation; embed + dev proxy + Make/CI wiring. | 1.5–2 weeks |
| **3. Read views** | Dashboard, Note view (bodyHTML + mermaid + enhancers), Tasks (read), Activity, Search (ranked+highlighted), Tags/Orphans, Graph (ported canvas). | 2–3 weeks |
| **4. Writes** | Interactive tasks (optimistic + rollback); note save (pessimistic + **409 flow**); **note/folder creation** (new). | 1–1.5 weeks |
| **5. Editor** | Split-preview parity first; CM6 left-pane + `[[ ]]`/`#tag`/`+project` autocomplete *if demanded* (lazy-loaded). | 1–2 weeks (CM6 optional, can defer) |
| **6. Regression + cutover** | Playwright suite (§5.5), perf at 5k notes, parity sign-off, flag flip, remove v1. | 1–1.5 weeks |
| **Desktop** | Verify SPA renders in Wails via existing handler-wrap (should be ~free). | 1–2 days |

**Total: ~8–12 weeks** for parity + the real Tier-1 gaps (note creation, ranked search), with CM6 autocomplete as an optional tail. Greenfield big-bang lands at the top of that range with more cutover risk; incremental islands spread it but ship value sooner.

---

## 8. Final recommendation (honest, even though it qualifies the rebuild)

**Do not do a greenfield SPA rebuild now. Adopt the JSON API now; defer the framework until a concrete feature demands it.**

The stated driver is "future-proofing," and there is **no concrete feature trigger** in any of the docs I read that the current htmx + read-model + typed-SSE adapter cannot reach. The roadmap in `web-industry-leading.md` explicitly notes that the headline Tier 1–2 items — interactive tasks, the lost-update guard, the split-preview editor, the force-directed graph, activity timeline, agent-memory home — are **already shipped on the no-build stack.** The remaining gaps (note/folder creation, ranked search, properties editing, split panes) are all reachable in htmx/vanilla without a framework. Rebuilding now spends 8–12 weeks and permanently sacrifices "one binary, no toolchain, fully offline" — the *exact* property that `web-industry-leading.md` identifies as your category-defining moat against Basic Memory et al. — to reach a place you're largely already at.

A framework earns its keep when **client-side state complexity** crosses a threshold the current app hasn't clearly hit: think real-time collaborative editing, multi-pane synchronized state, a plugin/extension API with third-party UI, complex drag-and-drop boards, or an offline-sync data layer. **Those are the legitimate triggers.** None is on the table yet.

So my professional recommendation, in order of preference:

1. **Best (do now, cheap, reversible): formalize the JSON API (§2) and keep htmx.** This is ~80% of the rebuild's durable value (a clean, typed, tested contract; tygo types; pagination; ranked search) at ~15% of the cost and **zero** sacrifice of the no-build/offline/single-binary properties. It is *also* the exact groundwork the SPA needs later, so it's never wasted. It's the move ADR 0001 itself anticipated as "Option D groundwork."
2. **If the client still wants the SPA: do it incrementally (§5.1), not greenfield.** Mount Svelte islands behind that JSON API one view at a time, keeping v1 as the live reference, so you never lose the v1 correctness net in one jump. Same end state, far less cutover risk.
3. **Greenfield big-bang: lowest preference.** It maximizes the window where bugs #1–#5 can silently regress and you pay the full toolchain cost before shipping any user-visible gain.

If a concrete trigger *does* land (e.g., "we need collaborative multi-cursor editing" or "a third-party plugin UI"), the stack in §1 is the one I'd reach for, the API in §2 is the contract I'd build on, and the incremental path in §5 is how I'd execute it — **with eyes open about §6.** Until then, the most senior thing I can tell you is that the current architecture is not the problem the rebuild is solving, and "future-proofing" without a feature is how teams trade a unique strength for a common one.

---

## References

- Current adapter: `internal/web/server.go`, `render.go`, `readmodel.go`, `assets/{app.js,graph.js,layout.html,style.css}`
- Prior decision being superseded: [ADR 0001](adr/0001-web-frontend-and-desktop.md)
- Roadmap / landscape: [web-industry-leading.md](web-industry-leading.md), [web-architecture-review.md](web-architecture-review.md), [web-read-model-plan.md](web-read-model-plan.md)
- Product values + write contract: SPEC §1, §6 (esp. §6.5), §12.1
- Domain: `internal/{mutate,task,note,links,search,store}` — writes via `mutate.Engine.Apply`
- Desktop: [`desktop/README.md`](../desktop/README.md), `.goreleaser.yaml`, `.github/workflows/{ci,release}.yml`
</content>
</invoke>
