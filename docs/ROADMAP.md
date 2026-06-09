# nt Roadmap — toward best-in-class notes + tasks (web & TUI)

North star: **make nt the best note-taking and task-manager app for both web and TUI** — without breaking the locked design (plain-text files, todo.txt, ULID-keyed undoable writes, AI-memory thesis; see [SPEC.md](../SPEC.md)).

This roadmap synthesizes four expert audits (web UX, TUI, task-management product, and Go core/architecture). Items keep their original F#/T#/W#/U#/E# ids for continuity.

## Strategic read (updated 2026-06-09)

The original gap — "nt is a task *list*, not a task *manager*" — is **closed**, and so is essentially the rest of the audit backlog. Across v0.1→v0.6 the planner (start/defer dates, Today/Agenda, recurrence, sub-tasks, dependency-cycle detection, time-of-day, full A–Z priority, time tracking, weekly review), the web app (CodeMirror editor, ranked search, daily-notes journal, mobile/PWA, command palette, a11y, force-graph, notes grid, move-between-folders), the TUI (command palette, vim counts, fuzzy jump, in-pane capture, redo, light theme), and the infrastructure (incremental read-model, in-memory search, shared query/DTO layer, config file, bounded payloads, god-file splits, cross-platform file locking) all shipped over the same plain-text store.

What's left is a **short polish/scale tail** — no remaining item is load-bearing for the core thesis. The one genuinely unstarted *feature* is **T4** (saved smart views); the rest are partial refinements (tasks in web search, web property editing, fuzzy filter ranking, more concurrency tests, TUI self-write de-flicker) and one deliberately-deferred scale item (**E2** persisted index, which only pays off past ~10k notes).

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
- **T5** bulk ops — `nt update`, `nt tag` (retag), `nt rm`
- **T6** time tracking — `est:` estimates + `nt start`/`nt stop` logging elapsed into `spent:`
- **T7** weekly review — `nt review`: overdue, stale, undated, stuck projects
- **T8** time-of-day on due dates (`--due "fri 5pm"`, ISO) ([#26](https://github.com/navbytes/nt/pull/26))
- **T9** full A–Z priority model

**Web** (SPA is the default; old htmx UI removed)
- Obsidian-class force-graph; **P3** create-notes-from-web; **P4** interactive task rows + agenda view
- **W1** CodeMirror 6 editor — markdown highlighting + `[[` wikilink autocomplete ([#33](https://github.com/navbytes/nt/pull/33))
- **W2** ranked note search + highlighted snippets ([#21](https://github.com/navbytes/nt/pull/21)) *(note bodies; task results still pending — see below)*
- **W3** daily notes / journal — web route + `nt journal` ([#30](https://github.com/navbytes/nt/pull/30))
- **W4** mobile shell — hamburger + off-canvas drawer ([#23](https://github.com/navbytes/nt/pull/23))
- **W5** command palette — all routes + actions + listbox a11y ([#20](https://github.com/navbytes/nt/pull/20))
- **W6** editor slash commands (`/todo`, `/table`, `/date`, …) alongside `[[` autocomplete
- **W7** a11y pass — skip-link, palette focus-trap, reduced-motion, aria-labels ([#24](https://github.com/navbytes/nt/pull/24))
- **W8** backlinks shown while editing
- move a note between folders — picker on the note page + `Move note…` palette action; `[[links]]` rewritten ([#59](https://github.com/navbytes/nt/pull/59))
- notes grid view (`/notes`) — folder filter, sort, Cards/Compact density toggle ([#60](https://github.com/navbytes/nt/pull/60))
- display-only truncation of long task text in the graph + lists ([#58](https://github.com/navbytes/nt/pull/58))
- PWA + app icon; stable default port

**TUI**
- **U1** live filter (search-as-you-type)
- **U2** command palette (`:`) — fuzzy action list
- **U3** fuzzy "go to anything" jumper + `L` multi-link picker fix
- **U4** fast capture + in-pane body textarea (no more two-step `$EDITOR`)
- **U5** set-doing from the keyboard
- **U6** vim count prefixes (`5j`/`12G`) with tabs moved to `[`/`]`
- **U8** light theme via adaptive palette ([#29](https://github.com/navbytes/nt/pull/29))
- **U9** explicit redo key + undo affordance

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
| **T4** | saved smart views (`nt view save/recall`; presets) | core/cli | S/M | next — in progress |
| W2 | add **tasks** to search results (notes are ranked already) | web | S | partial |
| W8 | frontmatter/properties **editing** in the web editor (backlinks-while-editing done) | web | M | partial |
| U1 | rank the live filter fuzzily, not by substring (filter itself shipped) | tui | S | partial |
| F2 | broaden concurrency tests — concurrent add/done/archive/undo (lock + store tests done) | core | M | partial |
| F4 | TUI self-write suppression (watcher reloads its own writes → flicker) | tui | M | not started |
| E2 | **persisted** on-disk index (cold start still re-reads all; in-memory shipped) | infra | L | deferred — only matters >10k notes |

## Suggested sequencing for what's left

1. **T4 saved views** — the one remaining feature; pairs naturally with the config file (a `[views]` section / views file). Mirror across CLI → TUI/web (or defer a surface explicitly).
2. **Search + editor polish** — W2 (tasks in web search), W8 (web property editing), U1 (fuzzy filter ranking). Small, high-visibility.
3. **Robustness** — F2 (concurrency stress) and F4 (TUI de-flicker).
4. **Scale, only when needed** — E2 persisted index. Defer until a real >10k-note store shows a cold-start cost.

Each surface change should be mirrored across CLI, TUI, web, and MCP (or explicitly deferred) — ideally via the shared query/DTO layer (E3) — to avoid the drift the core audit flagged.
