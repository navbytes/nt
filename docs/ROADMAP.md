# nt Roadmap — toward best-in-class notes + tasks (web & TUI)

North star: **make nt the best note-taking and task-manager app for both web and TUI** — without breaking the locked design (plain-text files, todo.txt, ULID-keyed undoable writes, AI-memory thesis; see [SPEC.md](../SPEC.md)).

This roadmap synthesizes four expert audits (web UX, TUI, task-management product, and Go core/architecture). Items are tagged by surface and effort (S < ½ day · M ≈ 1–2 days · L > 2 days). Most additions are **losslessly representable** in the existing todo.txt model as `key:value` conventions.

## Strategic read

nt's foundation is genuinely strong: clean ULID-keyed mutation engine, undo journal, provenance/source field, MCP + AI-sync, a just-shipped Obsidian-class graph view. The gap to "best app" is concentrated in **table-stakes task + editing flows**, and several capabilities are **already built in the backend but unused by the surfaces** (`dateparse`, `apiTaskStatus`/`apiTaskDelete`, `Task.Blocker`, `parent:`). The single biggest theme across three of four audits: **nt is a task *list*, not a task *manager*** — no start/defer date, no Today/Agenda view, and quick-add throws away structure.

## Now (in flight / next)

| # | Item | Surface | Effort | Status |
|---|------|---------|--------|--------|
| F1 | **Undo correctness** — post-image validation, no-resurrect, durable ordering + dir-fsync (SPEC §6.3) | core | M | ✅ PR #8 |
| P1 | **Natural-language quick-add** — "ship report fri 5pm #work !high" → structured fields; parse chips. Backend `dateparse` mostly exists; web quick-add currently corrupts structure | core, web, tui, mcp | M | next |
| P2 | **Start/defer date `t:` + Today / Upcoming / Agenda views** — turns the list into a planner; fixes misuse of "blocked"/future-due | core, cli, web, tui | M | next |
| P3 | **Create notes from the web** — biggest web dead-end (`POST /api/notes`, "+ New note") | web | M | next |
| P4 | **Wire up unused task backend** — interactive rows (open/inline-edit/delete/status cycle), render blockers/deps/projects | web | M | next |

## Foundation / correctness (core audit)

- **F2 — Test the safety-critical core**: `lock`, `store`, `undo` had ~zero tests. (store tests landed with F1; lock + concurrent add/done/archive/undo next.) — M
- **F3 — Archive atomicity** (C4): crash between done.txt and tasks.txt renames duplicates tasks; make atomic or reconcile cross-file in `doctor`. — M
- **F4 — TUI self-write suppression** (C6, SPEC §6.5): TUI watcher reloads its own writes (flicker); add mtime/ignore-next. — M
- **F5 — `Resolve` prefix-only** (C5): currently matches prefix *and* suffix → ambiguous handles can hit the wrong task. — S
- **F6 — Stop swallowing read errors** (`notes, _ :=`, `doc, _ :=`): a corrupt store renders as "empty". — S
- **F7 — Recursive note-folder watch**: `watch.go` is non-recursive; foldered notes don't live-refresh. — S

## Task management (product audit)

- **T1 — Sub-tasks**: `parent:` is stored but never read. Indented display, `--tree`, progress rollup, carry through recurrence. — M
- **T2 — Recurrence correctness**: fixed-vs-floating (`rec:+3d`), roll-forward past today (no overdue spawn), month-end clamp, `nt skip`. — S/M
- **T3 — Dependency hardening**: `dep:`/blockedBy from the dependent side, cycle detection, `doctor` dangling-edge check, show-blocked-greyed. — M
- **T4 — Saved smart views**: `nt view save/recall`; presets (today, overdue, stuck). Config file, not tasks.txt. — S/M
- **T5 — Bulk ops on all mutating verbs** (update/rm/reschedule/retag accept many ids). — S
- **T6 — Time estimates + tracking** (`est:`, `nt start/stop` → `spent:`). — M
- **T7 — Weekly-review workflow** (`nt review`: stuck projects, stale, undated). — M
- **T8 — Time-of-day on dates + reminder hooks** (`due:…T17:00`, `nt agenda --json` for cron/launchd). — M
- **T9 — Multiple projects + full priority model** (JSON drops `Projects()[0]` only; A–Z collapsed to A/B/C). — S

## Web (web UX audit)

- **W1 — CodeMirror 6 editor** (highlighted source + `[[` autocomplete; keep server goldmark render). CM6 edits the buffer directly → no markdown-fidelity risk (rules out TipTap/Milkdown). — L
- **W2 — Ranked search + highlighted snippets + task search** (current search is an unranked title list). — M
- **W3 — Daily notes / journal** — makes the AI-memory positioning a visible product. — L
- **W4 — Mobile shell** (collapsible drawer; there is no mobile nav today). — M
- **W5 — Command palette: full nav + actions** (only knows 3 of 6 routes; no New note/New task/Today). — S
- **W6 — Slash commands in the editor** (depends on W1). — M
- **W7 — a11y pass** (palette as listbox + focus trap; aria-labels on icon buttons; skip-link; reduced-motion). — M
- **W8 — Properties/frontmatter editing + backlinks-while-editing.** — M

## TUI (TUI audit)

- **U1 — Live, fuzzy filter** (search-as-you-type; rank fuzzy not substring). Highest-ROI TUI item. — S
- **U2 — Command palette (`:` ex-line)** with fuzzy action list. — M
- **U3 — Fuzzy "go to anything" jumper + fix `L` to open the multi-link picker SPEC promises.** — M
- **U4 — Fast note capture + in-TUI body textarea** (capture is a two-step `$EDITOR` dance today). — M
- **U5 — Set "doing" status from the keyboard** (model supports it; unreachable today). — S
- **U6 — Vim counts + group jumps + `n`/`N`.** — M
- **U7 — Add `[[wikilink]]` to notes + create-note-from-unresolved-link** (`l` is tasks-only today). — M
- **U8 — Light theme + theming** (web has it; TUI is hardcoded Tokyo Night). — M
- **U9 — Explicit redo key + undo affordance.** — S
- **Bugs:** add-task-from-notes-tab is invisible (`selectTaskByID` searches only `m.flat`); non-recursive note watch (= F7).

## Enabling infrastructure (cross-cutting)

- **E1 — Incremental read-model**: web rebuilds the entire snapshot (reparse every note) on every change — O(N²)-ish; the scale cliff past ~10k notes. — L
- **E2 — Persisted note/link index** (inverted index): powers ranked search/snippets (W2), backlinks, and fixes E1. — L
- **E3 — Shared `Query` DSL + DTO layer** across cli/tui/web/mcp (today each re-derives grouping/status → drift). — M
- **E4 — Pagination/limits** on graph + ⌘K + search payloads. — M
- **E5 — Config file** (`$NT_DIR/config.toml`): themes, keybindings, saved views, defaults. — M
- **E6 — Split god-files** (`cli/commands.go` ~32KB, `web/server.go` ~23KB). — M

## Sequencing rationale

1. **Foundation first** (F1 done): never build product features on a broken write/undo contract.
2. **"Make tasks real"** (P1–P2 + T1–T2): the highest-consensus product gap; mostly lossless todo.txt additions; lands across CLI/TUI/web/MCP together so the surfaces stay consistent.
3. **Close web dead-ends** (P3–P4, W2, W5): cheap wins that wire up existing backend.
4. **Editor + scale** (W1, E1/E2): the larger investments, once the daily-driver flows are solid.

Each surface change must be mirrored across CLI, TUI, web, and MCP (or explicitly deferred) to avoid the drift the core audit flagged — ideally via the shared Query/DTO layer (E3).
