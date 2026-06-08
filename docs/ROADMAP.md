# nt Roadmap — toward best-in-class notes + tasks (web & TUI)

North star: **make nt the best note-taking and task-manager app for both web and TUI** — without breaking the locked design (plain-text files, todo.txt, ULID-keyed undoable writes, AI-memory thesis; see [SPEC.md](../SPEC.md)).

This roadmap synthesizes four expert audits (web UX, TUI, task-management product, and Go core/architecture). Items are tagged by surface and effort (S < ½ day · M ≈ 1–2 days · L > 2 days). Most additions are **losslessly representable** in the existing todo.txt model as `key:value` conventions.

## Strategic read

nt's foundation is genuinely strong: clean ULID-keyed mutation engine, undo journal, provenance/source field, MCP + AI-sync, a just-shipped Obsidian-class graph view. The gap to "best app" is concentrated in **table-stakes task + editing flows**, and several capabilities are **already built in the backend but unused by the surfaces** (`dateparse`, `apiTaskStatus`/`apiTaskDelete`, `Task.Blocker`, `parent:`). The single biggest theme across three of four audits: **nt is a task *list*, not a task *manager*** — no start/defer date, no Today/Agenda view, and quick-add throws away structure.

## ✅ Shipped (as of this push — 16 merged PRs)

- **Foundation:** F1 undo correctness (post-image validation, no-resurrect, durable ordering) + store dir-fsync + first `store` tests; F7 recursive note watch; T3 dependency cycle detection + dangling-edge `doctor` checks (no more invisible deadlock); CI/security (fixed broken vuln-scan, patched stdlib CVEs via Go 1.25.11, path-injection hardening).
- **Tasks:** P1 natural-language quick-add across CLI/TUI/web/MCP; P2 start/defer `t:` + `nt today`/`nt agenda` + hide-future-start; T2 recurrence correctness (strict vs floating, roll-forward, month clamp) + `nt skip`; T5 bulk `update`; T1 `nt list --tree` sub-tasks; T9 full A–Z priority; dateparse tests.
- **Web:** P3 create-notes-from-web; P4 interactive task rows + agenda (date-grouped) view; W2 ranked search + highlighted snippets; W5 command palette (all routes + actions + listbox a11y); PWA + app icon; stable default port.
- **TUI:** U1 live filter; U5 set-doing key; add-from-notes-tab fix.

## Next up

| # | Item | Surface | Effort |
|---|------|---------|--------|
| W1 | **CodeMirror 6 editor** — highlighted source + `[[` autocomplete (keep server goldmark render) | web | L |
| W3 | **Daily notes / journal** — makes the AI-memory positioning a visible product | web | L |
| W4 | **Mobile shell** — collapsible drawer (no mobile nav today) | web | M |
| W7 | **a11y pass (beyond the palette)** — focus trap, skip-link, reduced-motion, aria-labels | web | M |
| U2/U3 | **TUI command palette + fuzzy jumper** (fix `L` multi-link picker) | tui | M |
| U8 | **TUI light theme** | tui | M |
| T8 | **Time-of-day on dates + reminder hooks** | core | M |
| F3/F4/F6 | Archive atomicity; TUI self-write suppression; surface read errors | core/tui | M |
| E1/E2 | **Incremental read-model + persisted search index** — the scale work (>10k notes) | infra | L |
| E3/E5 | Shared Query DSL across surfaces; `$NT_DIR/config.toml` | infra | M |

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
