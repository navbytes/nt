# nt Roadmap — toward best-in-class notes + tasks (web & TUI)

North star: **make nt the best note-taking and task-manager app for both web and TUI** — without breaking the locked design (plain-text files, todo.txt, ULID-keyed undoable writes, AI-memory thesis; see [SPEC.md](../SPEC.md)).

This roadmap synthesizes four expert audits (web UX, TUI, task-management product, and Go core/architecture). Items are tagged by surface and effort (S < ½ day · M ≈ 1–2 days · L > 2 days) and keep their original F#/T#/W#/U#/E# ids for continuity.

## Strategic read (updated)

The original gap — "nt is a task *list*, not a task *manager*" — is **closed**: start/defer dates, Today/Agenda, recurrence, sub-tasks, dependency hardening, and structured quick-add all shipped across CLI/TUI/web/MCP. The **web is now essentially feature-complete** (real CodeMirror editor, ranked search, daily-notes journal, mobile shell, command palette, a11y, PWA) and the read-model is **incremental** (scales past the snapshot-rebuild cliff). The remaining work has shifted to three pockets: **(1) the TUI**, now the thinnest surface; **(2) a correctness/quality tail** (a `Resolve` ambiguity bug, lock/undo tests); and **(3) architecture** (shared query layer, config file, god-file splits) that pays down drift risk rather than adding features.

## ✅ Shipped

**Foundation / correctness**
- **F1** undo correctness — post-image validation, no-resurrect, durable ordering + store dir-fsync + first `store` tests
- **F3** archive atomicity — `doctor` reconciles cross-file (crash-leftover) duplicates ([#25](https://github.com/navbytes/nt/pull/25))
- **F6** surface store read errors (red banner) instead of rendering an empty store ([#28](https://github.com/navbytes/nt/pull/28))
- **F7** recursive note-folder watch
- **T3** dependency hardening — cycle detection (no invisible deadlock) + dangling-edge `doctor` checks ([#19](https://github.com/navbytes/nt/pull/19))
- CI/security — fixed the broken vuln-scan action, patched reachable stdlib CVEs (Go 1.25.11), path-injection hardening on note create

**Tasks → a real planner**
- **P1** natural-language quick-add across CLI/TUI/web/MCP
- **P2** start/defer `t:` + `nt today` / `nt agenda` + hide-future-start
- **T1** sub-tasks: `parent:` read, `nt list --tree`, progress rollup
- **T2** recurrence correctness (strict vs floating, roll-forward, month-end clamp) + `nt skip`
- **T5** bulk `nt update`
- **T8** time-of-day on due dates (`--due "fri 5pm"`, ISO) ([#26](https://github.com/navbytes/nt/pull/26))
- **T9** full A–Z priority model

**Web** (SPA is the default; old htmx UI removed)
- Obsidian-class force-graph; **P3** create-notes-from-web; **P4** interactive task rows + agenda view
- **W1** CodeMirror 6 editor — markdown highlighting + `[[` wikilink autocomplete ([#33](https://github.com/navbytes/nt/pull/33))
- **W2** ranked search + highlighted snippets ([#21](https://github.com/navbytes/nt/pull/21))
- **W3** daily notes / journal — web route + `nt journal` ([#30](https://github.com/navbytes/nt/pull/30))
- **W4** mobile shell — hamburger + off-canvas drawer ([#23](https://github.com/navbytes/nt/pull/23))
- **W5** command palette — all routes + actions + listbox a11y ([#20](https://github.com/navbytes/nt/pull/20))
- **W7** a11y pass — skip-link, palette focus-trap, reduced-motion, aria-labels ([#24](https://github.com/navbytes/nt/pull/24))
- PWA + app icon; stable default port

**TUI**
- **U1** live filter (search-as-you-type); **U5** set-doing from the keyboard; **U8** light theme via adaptive palette ([#29](https://github.com/navbytes/nt/pull/29)); add-from-notes-tab bug fix

**Infrastructure**
- **E1** incremental read-model — mtime parse cache; a 2000-note rebuild dropped 37.9ms → 6.7ms ([#34](https://github.com/navbytes/nt/pull/34))
- **E2** in-memory search — title-ranked + snippets with no per-query ripgrep ([#34](https://github.com/navbytes/nt/pull/34))

## ⬜ Remaining

| # | Item | Surface | Effort | Status |
|---|------|---------|--------|--------|
| **F5** | `Resolve` matches prefix **and** suffix → an ambiguous handle can hit the wrong task | core | S | ⚠️ correctness bug, untouched |
| F2 | finish safety-critical tests: `lock` + concurrent add/done/archive/undo (store done) | core | M | partial |
| F4 | TUI self-write suppression (watcher reloads its own writes → flicker) | tui | M | not started |
| T4 | saved smart views (`nt view save/recall`; presets) | core/cli | S/M | not started |
| T5 | bulk ops on the remaining verbs (`rm`/`retag`/`reschedule`; `update` done) | cli | S | partial |
| T6 | time estimates + tracking (`est:`, `nt start/stop` → `spent:`) | core | M | not started |
| T7 | weekly review (`nt review`: stuck / stale / undated) | cli | M | not started |
| W2 | add **tasks** to search results (notes are ranked already) | web | S | partial |
| W6 | slash commands in the editor (now unblocked by W1) | web | M | not started |
| W8 | frontmatter/properties editing + backlinks-while-editing | web | M | not started |
| U2 | TUI command palette (`:` ex-line, fuzzy action list) | tui | M | not started |
| U3 | fuzzy "go to anything" jumper + fix `L` multi-link picker | tui | M | not started |
| U4 | fast note capture + in-TUI body textarea (today: two-step `$EDITOR`) | tui | M | not started |
| U6 | vim counts + `n`/`N` (needs a keymap decision — `1`/`2`/`3` are tab keys) | tui | M | not started |
| U7 | add `[[wikilink]]` / create-note-from-link in the TUI | tui | M | not started |
| U9 | explicit redo key + undo affordance | tui | S | not started |
| U1 | rank the live filter fuzzily, not by substring (filter itself shipped) | tui | S | partial |
| E2 | **persisted** on-disk index (cold start still re-reads all; in-memory shipped) | infra | L | partial |
| E3 | shared Query/DTO layer across cli/tui/web/mcp (each re-derives grouping today) | infra | M | not started |
| E4 | pagination/limits on graph + ⌘K + search payloads | infra | M | not started |
| E5 | config file (`$NT_DIR/config.toml`: themes, keybindings, saved views) | infra | M | not started |
| E6 | split god-files (`cli/commands.go`, `web/server.go`) | infra | M | not started |

## Suggested sequencing for what's left

1. **Correctness quick wins** — F5 (real bug), U9 (redo), T5-rest (bulk `rm`/`retag`). Small, high-confidence.
2. **TUI parity batch** — U2/U3/U4/U7 bring the TUI up to the web's polish (the biggest remaining surface gap).
3. **Product depth** — T6/T7 (estimates + weekly review), W6/W8 (editor slash-commands + frontmatter).
4. **Architecture** — E3 (shared query layer) then E5/E6, to pay down drift before the surface count grows further. E2-persisted and E4 only matter at real scale (>10k notes).

Each surface change should be mirrored across CLI, TUI, web, and MCP (or explicitly deferred) — ideally via the shared Query/DTO layer (E3) — to avoid the drift the core audit flagged.
