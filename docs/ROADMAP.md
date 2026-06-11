# nt Roadmap — toward best-in-class notes + tasks (web & TUI)

North star: **make nt the best note-taking and task-manager app for both web and TUI** — without breaking the locked design (plain-text files, todo.txt, ULID-keyed undoable writes, AI-memory thesis; see [SPEC.md](../SPEC.md)).

This roadmap synthesizes four expert audits (web UX, TUI, task-management product, and Go core/architecture). Items keep their original F#/T#/W#/U#/E# ids for continuity.

## Strategic read (updated 2026-06-09)

The original gap — "nt is a task *list*, not a task *manager*" — is **closed**, and so is essentially the rest of the audit backlog. Across v0.1→v0.6 the planner (start/defer dates, Today/Agenda, recurrence, sub-tasks, dependency-cycle detection, time-of-day, full A–Z priority, time tracking, weekly review), the web app (CodeMirror editor, ranked search, daily-notes journal, mobile/PWA, command palette, a11y, force-graph, notes grid, move-between-folders), the TUI (command palette, vim counts, fuzzy jump, in-pane capture, redo, light theme), and the infrastructure (incremental read-model, in-memory search, shared query/DTO layer, config file, bounded payloads, god-file splits, cross-platform file locking) all shipped over the same plain-text store.

What's left is a **short polish/scale tail** — no remaining item is load-bearing for the core thesis: partial refinements (tasks in web search, web property editing, fuzzy filter ranking, more concurrency tests, TUI self-write de-flicker) and one deliberately-deferred scale item (**E2** persisted index, which only pays off past ~10k notes). With **T4** (saved smart views) now shipped, there is no net-new *feature* left on the backlog.

## 🧭 Next up — grouped roadmap (2026-06-11)

The audit backlog is closed; this is the forward plan. Items are grouped by
surface, sized S/M/L, and sequenced so each lands as its own green-CI PR.

### Group D — Desktop parity (goal: at least as rich as the web)

| # | Item | Effort | Status |
|---|------|--------|--------|
| D1 | Enable editing in the desktop shell (`SetEdit(true)`) — full web UX (quick-add, complete/reschedule/undo, editor) with a stronger trust model (no TCP port at all) | S | ✅ |
| D2 | Webview-safe dialogs — replace every `prompt()`/`alert()`/`confirm()` (webviews don't implement them): inline sidebar note creation, palette "New task" → quick-add focus, Board delete → undo toast | M | ✅ |
| D3 | macOS App + Edit menus (⌘C/⌘V are dead keys in WKWebView without one), system light/dark appearance, standard title bar (no traffic-light overlap) | S | ✅ |
| D4 | External links from notes open in the system browser, not inside the webview (no back button there); web gets `target=_blank rel=noopener` on external links too | M | ✅ |
| D5 | Window-state persistence (size/position across launches) — needs Wails bindings | M | ⬜ |
| D6 | About panel + version, app-icon polish | S | ⬜ |

### Group W — Web polish

| # | Item | Effort | Status |
|---|------|--------|--------|
| W9 | Frontmatter/properties editing in the web editor (the roadmap's long-standing W8 partial) | M | ⬜ |
| W10 | Today "plan my day": capacity bar summing `est:` against a configurable daily budget | M | ⬜ |
| W11 | Bulk actions — `x` multi-select on task rows, then one keystroke completes/reschedules/deletes the set | M | ⬜ |
| W12 | Quick filter box on /tasks (`@tag +project text`, client-side, same grammar as quick-add chips) | S | ⬜ |

### Group T — TUI parity

| # | Item | Effort | Status |
|---|------|--------|--------|
| T10 | Saved views in the TUI — a view picker in the `:` palette running the shared `view.Apply` | M | ✅ |
| T11 | One-key reschedule in the TUI — covered: `D` already opens a due prompt with NL dates (today/fri/+3d); a `d` menu would clash with the `dd` done-chord | S | ✖ dropped |

### Group R — Release & docs

| # | Item | Effort | Status |
|---|------|--------|--------|
| R1 | CI compile check for the desktop module on PRs + a `web` CI job (svelte-check + vitest — the frontend had no CI at all) | S | ✅ |
| R2 | README: desktop app install section (bundles exist per release but the README never mentions them) | S | ✅ |
| R3 | Refresh docs/screenshots (pre-date the priority chips / relative dates / views sidebar) | S | ⬜ |

Sequencing: D1–D3 (one PR, done) → D4+R1+R2 (one PR) → T10(+T11) → W10 → W11 → W12 → W9 → D5/D6/R3 as time allows. Each PR merges only on green CI.

## ✅ Shipped

**Foundation / correctness**
- **F1** undo correctness — post-image validation, no-resurrect, durable ordering + store dir-fsync + first `store` tests
- **F3** archive atomicity — `doctor` reconciles cross-file (crash-leftover) duplicates ([#25](https://github.com/navbytes/nt/pull/25))
- **F5** `Resolve` is suffix-only — a shared ULID timestamp prefix can no longer match the wrong task
- **F6** surface store read errors (red banner) instead of rendering an empty store ([#28](https://github.com/navbytes/nt/pull/28))
- **F7** recursive note-folder watch
- **T3** dependency hardening — cycle detection (no invisible deadlock) + dangling-edge `doctor` checks ([#19](https://github.com/navbytes/nt/pull/19))
- cross-platform file lock — `flock` on Unix, `LockFileEx` on Windows, behind a build-tagged seam ([#61](https://github.com/navbytes/nt/pull/61)); nt now builds + ships on Windows too
- CI/security — fixed the broken vuln-scan action, patched reachable stdlib CVEs (Go 1.25.11), path-injection hardening on note create/move

**Tasks → a real planner**
- **P1** natural-language quick-add across CLI/TUI/web/MCP
- **P2** start/defer `t:` + `nt today` / `nt agenda` + hide-future-start
- **T1** sub-tasks: `parent:` read, `nt list --tree`, progress rollup
- **T2** recurrence correctness (strict vs floating, roll-forward, month-end clamp) + `nt skip`
- **T4** saved smart views — `nt view save/recall/list/rm`, persisted to `$NT_DIR/views.json` via a shared `internal/view` package (a saved view filters/sorts identically to the equivalent `nt list` flags)
- **T5** bulk ops — `nt update`, `nt tag` (retag), `nt rm`
- **T6** time tracking — `est:` estimates + `nt start`/`nt stop` logging elapsed into `spent:`
- **T7** weekly review — `nt review`: overdue, stale, undated, stuck projects
- **T8** time-of-day on due dates (`--due "fri 5pm"`, ISO) ([#26](https://github.com/navbytes/nt/pull/26))
- **T9** full A–Z priority model

**Web** (SPA is the default; old htmx UI removed)
- Obsidian-class force-graph; **P3** create-notes-from-web; **P4** interactive task rows + agenda view
- **W1** CodeMirror 6 editor — markdown highlighting + `[[` wikilink autocomplete ([#33](https://github.com/navbytes/nt/pull/33))
- **W2** ranked search + highlighted snippets ([#21](https://github.com/navbytes/nt/pull/21)) — notes (title + body) and tasks, tasks badged + linked to the task list ([#64](https://github.com/navbytes/nt/pull/64))
- **W3** daily notes / journal — web route + `nt journal` ([#30](https://github.com/navbytes/nt/pull/30))
- **W4** mobile shell — hamburger + off-canvas drawer ([#23](https://github.com/navbytes/nt/pull/23))
- **W5** command palette — all routes + actions + listbox a11y ([#20](https://github.com/navbytes/nt/pull/20))
- **W6** editor slash commands (`/todo`, `/table`, `/date`, …) alongside `[[` autocomplete
- **W7** a11y pass — skip-link, palette focus-trap, reduced-motion, aria-labels ([#24](https://github.com/navbytes/nt/pull/24))
- **W8** backlinks shown while editing
- move a note between folders — picker on the note page + `Move note…` palette action; `[[links]]` rewritten ([#59](https://github.com/navbytes/nt/pull/59))
- notes grid view (`/notes`) — folder filter, sort, Cards/Compact density toggle ([#60](https://github.com/navbytes/nt/pull/60))
- display-only truncation of long task text in the graph + lists ([#58](https://github.com/navbytes/nt/pull/58))
- task legibility — colour-coded A/B/C priority (chip + accent bar, WCAG-AA in both themes), relative due dates (“Today”/“Tomorrow”/“3d ago”, ISO on hover), within-bucket urgency sort, and an agent-source badge that surfaces AI-captured tasks while hiding noisy `cli`/`web` origins
- keyboard fluency — `g`-prefixed go-to chords (`g t/a/r/n/d/g/v`), `/` search, `c` capture, and a `?` shortcut cheat-sheet (modal, focus-managed); fixed the last command-palette `tabindex` a11y gap
- quick-add live parse preview — the todo.txt shorthand resolves to priority/due/project/tag/link chips as you type, mirroring the server grammar (`internal/quickadd` + `task.ParseLine`) so the preview never drifts from what gets saved
- one-keystroke rescheduling — `d` (or the row's ⏱ button) opens Today/Tomorrow/Next week/Clear; `POST /api/tasks/{id}` accepts NL `due`/`pri` resolved by `dateparse` server-side
- undo toast — complete/delete act instantly with a 6s "— Undo" toast wired to `POST /api/undo` (the same transactional engine as `nt undo`; refuses with 409 rather than corrupting if the store moved); the delete `confirm()` is gone
- **saved views surfaced in the web** — `nt view` specs appear in a sidebar Views section; `/tasks?view=<name>` applies them server-side via the new shared `view.Apply` (the CLI's `keep`/`sortTasks` moved into `internal/view`, so a named view can never filter differently across surfaces)
- editor live preview renders Mermaid (shared `lib/mermaid` runner with the note page; theme-aware, including OS-dark with no saved toggle)
- PWA + app icon; stable default port

**TUI**
- **U1** live filter (search-as-you-type) — fuzzy subsequence matching, fzf-style space-separated AND terms (so `fmb` finds "fix my bug")
- **U2** command palette (`:`) — fuzzy action list
- **U3** fuzzy "go to anything" jumper + `L` multi-link picker fix
- **U4** fast capture + in-pane body textarea (no more two-step `$EDITOR`)
- **U5** set-doing from the keyboard
- **U6** vim count prefixes (`5j`/`12G`) with tabs moved to `[`/`]`
- **U8** light theme via adaptive palette ([#29](https://github.com/navbytes/nt/pull/29))
- **U9** explicit redo key + undo affordance
- **F4** TUI self-write suppression — a content signature skips the watcher's ~80ms echo of our own writes, so mutating no longer flickers/re-sorts the view

**Infrastructure**
- **E1** incremental read-model — mtime parse cache; a 2000-note rebuild dropped 37.9ms → 6.7ms ([#34](https://github.com/navbytes/nt/pull/34))
- **E2** in-memory search — title-ranked + snippets with no per-query ripgrep ([#34](https://github.com/navbytes/nt/pull/34))
- **E3** shared query/DTO layer — `task.VisibleInList` / `DueBucket` / `CompletedSince` consumed by cli, tui, web, and mcp (no more per-surface re-derivation)
- **E4** bounded graph + search + ⌘K payloads with truncation signals (large stores)
- **E5** config file — `$NT_DIR/config.toml` (`[defaults]`, `[web]`, `[tui]`); flags + env override
- **E6** split god-files (`cli/commands.go`, `web/server.go`)
- **E7** desktop app (Wails) — macOS/Linux/Windows bundles built per release tag

## ⬜ Remaining

| # | Item | Surface | Effort | Status |
|---|------|---------|--------|--------|
| W8 | frontmatter/properties **editing** in the web editor (backlinks-while-editing done) | web | M | partial |
| F2 | broaden concurrency tests — concurrent add/done/archive/undo (lock + store tests done) | core | M | partial |
| E2 | **persisted** on-disk index (cold start still re-reads all; in-memory shipped) | infra | L | deferred — only matters >10k notes |

## Suggested sequencing for what's left

1. **Editor polish** — W8 (frontmatter/properties editing in the web editor). Small, high-visibility.
2. **Robustness** — F2 (concurrency stress: concurrent add/done/archive/undo).
3. **Surface the saved views** — ✅ web shipped (sidebar Views section + server-side `view.Apply`); the TUI can adopt the same shared filter next. Optional follow-up.
4. **Scale, only when needed** — E2 persisted index. Defer until a real >10k-note store shows a cold-start cost.

Each surface change should be mirrored across CLI, TUI, web, and MCP (or explicitly deferred) — ideally via the shared query/DTO layer (E3) — to avoid the drift the core audit flagged.
