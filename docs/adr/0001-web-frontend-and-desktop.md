# ADR 0001 — Web frontend strategy and the desktop path

- **Status:** Accepted
- **Date:** 2026-06-08
- **Context owner:** nt maintainers

## Context

`nt web` grew quickly — from a read-only viewer to a UI with a command palette,
search-as-you-type, a `/tasks` dashboard, a `/graph` view, hover previews,
syntax highlighting, and opt-in editing. That raised two questions:

1. Should the web UI adopt a "proper" frontend framework (React/Vue/Svelte) to
   stay maintainable as it grows?
2. We may want a desktop app (e.g. **Wails**) later. Does that change the
   frontend choice we should make now?

### What the code actually is

| Layer | Size | Notes |
|---|---|---|
| Go (`internal/web/server.go` + `render.go`) | ~1,050 LOC | thin adapter over the shared domain |
| `assets/app.js` | ~350 LOC | one vanilla IIFE, progressive enhancement |
| `assets/style.css` | ~570 LOC | hand-rolled design system |
| `assets/layout.html` | ~216 LOC | server-side `html/template` |
| vendored | mermaid only | gzipped, embedded, **zero external requests** |

The decisive architectural fact: **the web layer is an adapter in nt's
hexagonal architecture.** All real logic (`task`/`note`/`links`/`search`/
`store`/`mutate`) is shared with the CLI, TUI, and MCP server. The entire
rendering surface that could ever be rewritten is ~1,400 LOC of glue.

### Product values at stake

nt's identity is: plain files, **single static Go binary, no build step, no
external requests, works fully offline, localhost-only**. Server-rendered HTML +
`go:embed` is *aligned* with that thesis, not a compromise tolerated for now.

## Decision

### 1. Do **not** adopt an SPA framework for `nt web` now.

At ~350 LOC of vanilla JS over a thin Go adapter, the UI is at the
early-warning point, not a maintenance crisis. A React/Vue/Svelte SPA would
introduce a `node_modules` build pipeline into the repo and CI, require
contributors to install npm, and trade a current strength (no build, offline,
single binary) for parity with where we already are. The bar for that is high
and unmet.

### 2. Fix the one real maintainability smell with no-build tools.

The fragile part is **client-side `innerHTML` string-building** (palette,
search, recently-viewed assemble markup from JS strings). The proportionate
remedies, all single-file / no-build / offline-safe:

- **htmx** (~14 KB, vendored) — the natural fit for a Go server-rendered app:
  keep rendering HTML on the server, swap fragments. Removes most JS string
  templating. Highest leverage, lowest cost. *(Preferred when we act.)*
- **Alpine.js** (~15 KB) — if we want sprinkled reactivity instead of fragment
  swaps.
- Split `app.js` into `<script type="module">` files and document the JSON
  endpoints that already exist (`?json=1`, `?preview=1`, `?raw=1`) as a small
  API.

### 3. Treat the desktop app as the only thing that could justify a bigger investment — and validate it with a spike before committing.

`nt web` today is **server-rendered**. Wails' native model is a
**client-rendered** frontend bound to Go methods — a *different shape*. So we
must not half-adopt a framework "to be Wails-ready"; Wails imposes no framework,
and a premature SPA rewrite would discard the lean server-rendered approach that
is perfect for `nt web` today.

We built a spike (`/desktop`, ADR-accompanying) to measure the real effort.
**Finding: Wails can render the existing server-rendered UI as-is** by passing
the existing `http.Handler` to `assetserver.Options{Handler: ...}`. The native
window over the *entire* current UI took ~60 lines plus one exported method
(`web.Server.Handler()`). Built with `go build -tags production`, it links
natively against WebKit/Cocoa/AppKit (verified with `otool -L`). The
Wails/CGO/WebKit dependencies are isolated in a nested Go module, so the CLI's
single-static-binary story is untouched.

**Consequence:** shipping a desktop app does **not** require a frontend rewrite
first. A richer future desktop app *may* later move to Wails' Go-bindings model
with a client-rendered frontend — but that is an **additive** evolution we can
do when desktop is a committed product goal, not a prerequisite we pay for now.

## Options considered

- **A. Full SPA rewrite (React/Svelte/Vue) now.** Rejected: contradicts core
  product values; adds a build toolchain; unjustified at current complexity.
- **B. Status quo (vanilla JS, server-rendered).** Good, but the
  `innerHTML`-string smell will compound. Adopt the no-build improvements in
  Decision 2.
- **C. No-build progressive enhancement (htmx/Alpine) + server rendering.**
  **Chosen direction** for `nt web`: keeps every product value, removes the
  smell.
- **D. Decoupled client frontend + JSON API, shared by `nt web` and Wails.**
  The right move **if/when desktop becomes a committed goal** — build the UI
  once for both targets. Deferred; the hexagonal split makes the ~1,400-LOC
  adapter cheap to evolve later.

## Consequences

- The CLI keeps its no-build, single-binary, offline guarantees.
- `internal/web` gains a small embeddable seam (`Handler()`, `SetEdit()`,
  `StartWatch()`), useful beyond Wails.
- A `/desktop` spike module exists, isolated from the CLI's dependency graph and
  excluded from its CI/release. It is a proof of concept, not a shipped target.
- When desktop graduates from "prototype" to "committed," revisit Option D using
  the spike as the starting point.

## References

- Spike: [`/desktop`](../../desktop/README.md)
- Embeddable seam: `internal/web/server.go` — `Server.Handler/SetEdit/StartWatch`
