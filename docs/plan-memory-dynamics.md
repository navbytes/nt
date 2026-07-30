# Memory dynamics — implementation plan

**Status:** EXECUTED — all four phases implemented on this branch in the order
recommended below (3 → 1 → 2 → 4), plus an independent-review fix pass (8
confirmed findings) and a multi-agent simulation pass (Sonnet/Haiku sessions
sharing one store) whose three usability findings were also fixed. Deviations
from the plan: `RankProject` kept its signature with one `now` hoisted per
ranking pass instead of the threaded parameter (revisit at the next signature
churn); MCP `clear_half_life`/`clear_reviewed` not added (`half_life:"none"`
covers the decay opt-out); OpenCode's regex-and-prompt pass remains the
tracked follow-up flagged in Part C.
**Date:** 2026-07-30
**Branch:** `feat/memory-dynamics-spec`

This document turns the open spec into an ordered work breakdown: exact
packages, functions, tool-catalog entries, integration files, and tests per
phase. It is grounded in an investigation of the two integrations that must
carry the features to agents — the **Pi extension**
(`integrations/pi/extensions/nt-memory.ts`) and the **Claude Code plugin**
(`.claude-plugin/` + `.claude/skills/` + `nt hook`) — summarized first,
because several of its findings change what the implementation must touch
(and, just as usefully, what it must *not*).

---

## Part A — Integration investigation: findings that shape the plan

### A.1 Pi (`integrations/pi/`)

How it works today (from reading `nt-memory.ts`, `AGENTS.md`, `prompts/*.md`,
`embed.go`, `install.sh`):

1. **Tool registration is dynamic.** On load, `NtBridge` spawns `nt mcp`,
   calls `tools/list`, and registers **every** returned tool as a native Pi
   tool — names, descriptions, and schemas come from the binary at runtime.
   - **Consequence: new MCP tools (`nt_touch`, `nt_decide`, `nt_history`)
     appear in Pi with zero TypeScript changes.** The plan needs no Pi
     bridging work for new tools — only prompt/convention updates, plus the
     two regexes below.
2. **Write detection is two hardcoded regexes.** `NT_WRITE_TOOL`
   (`/^nt_(add|note|note_edit|update|tag|mv|rm|archive|relink)$/`) and
   `NT_WRITE_COMMAND` (the `nt note|add|…` bash-fallback pattern) drive two
   behaviors: compile-cache invalidation (so an edit this turn is injected
   this turn) and the idle-nudge's "did this session write anything" flag.
   - **Consequence: both regexes must gain `decide|touch`** or (a) a
     `nt decide` this turn won't refresh the injected rules/memory block when
     the decided note is a rule/memory-core note, and (b) a session whose
     only capture was a decision line still gets nudged to run `/learn` —
     wrong on both counts. This is the *only* mandatory TS change.
3. **Injection compiles from `nt export --tag rule` / `--tag memory-core`**,
   memoized by `nt store-hash` (stat-walk fingerprint). `nt decide`/
   `nt touch` write note files, so `store-hash` moves and the cache refreshes
   correctly with no new plumbing. Decay never touches injection: pinned
   layers (rules/, memory/) are exempt from tier-fade by spec §3.3, and the
   compiled block is tag-filtered, not recall-ranked.
4. **Error-triggered recall shells `nt recall --lessons-only --json`.**
   Ranking lives entirely in Go (`recall.RankProject`), so **decay applied
   inside the ranker reaches Pi's error recall automatically** — same for
   OpenCode and the Claude Code hook (A.2.3). No per-integration decay logic,
   by construction.
5. **Assets are embedded** (`embed.go`) and installed by `nt pi install`;
   `nt doctor --integrations` checks installed copies for drift.
   - **Consequence: every edit to `AGENTS.md` / `prompts/*.md` /
     `skills/nt/SKILL.md` ships through a rebuild + reinstall**, and the plan
     must keep the Pi copies and the Claude copies of shared conventions in
     lockstep (they are separate files with near-identical content).
6. **Resilience contracts to preserve:** hooks never throw; a broken nt can
   never take down a session; every bridged failure message tells the agent
   the CLI fallback. New tool responses (`escalate`, `matched`) must be
   additive JSON so old extensions render them harmlessly as text.

### A.2 Claude Code (`.claude-plugin/`, `.claude/skills/`, `nt hook`)

1. **The plugin is thin by design**: `plugin.json` registers the `nt mcp`
   server + the three skills (`nt`, `nt-learn`, `nt-distill`); the
   marketplace manifest wraps it. Like Pi, tool exposure is dynamic via MCP —
   **no plugin-manifest change is needed for new tools.** A version bump
   (`0.1.0` → `0.2.0`) signals the new surface.
2. **The skills are the behavior layer.** `.claude/skills/nt/SKILL.md` is
   where agents learn the capture/retrieve workflow (index-first, recall
   tiers, `--kind` filing, dedup rules); `nt-learn` is the approval-gated
   harvest; `nt-distill` the consolidation walk. **All three encode the exact
   conventions the spec changes** (create-vs-edit steering, what to do on
   `similar`, when to trust recall) — they are the main "documentation" line
   items in Part B, not afterthoughts.
3. **`nt hook` (Go) is the automatic loop**: TodoWrite mirroring plus
   Bash-failure lesson recall (`hookBashErrorRecall` →
   `recall.RankProject(lessons, query, 3, "")` → exit-2 block+reason).
   Decay in the ranker reaches it automatically; the call site gains only a
   `now` argument (B.1).
4. **A known CLI/MCP asymmetry the spec must respect:** CLI `nt note`
   *refuses* fuzzy near-duplicates (with a consolidation hint); MCP `nt_note`
   *always creates* and returns a `similar` list — a deliberate choice
   (parallel agents must never lose a capture). Spec §4's `if_exists` is a
   third, **exact-match** behavior (slug/title), orthogonal to fuzzy
   `FindSimilar`: it fires *before* creation on an exact topic hit, while
   `similar` continues to catch fuzzy near-dupes after creation. The plan
   keeps all three; `if_exists` defaults to `"create"` so the
   never-lose-a-capture contract is unbroken for existing callers.

---

## Part B — Work breakdown

Phases mirror the spec (§10). Phase 3 has no dependency on 1–2 and is the
smallest diff; recommended land order: **3 → 1 → 2 → 4**.

### B.1 Phase 1 — relevance decay (`half_life:` / `reviewed:`)

**Go core**

| File | Change |
|---|---|
| `internal/note/note.go` | Model `HalfLife string` + `Reviewed string` (parse in `parseFrontmatter`, emit in `Save` — mind `invalidFrontmatterLine`; both currently round-trip via `Extra`, so add migration-free promotion the way `valid_from` did). `ParseHalfLife(string) (time.Duration, ok, isNone)` accepting `Nd/Nw/Nm/Ny/none`. Extend `ChangedDate()`-style age basis: `AgeBasis()` = max(reviewed, updated, created, mtime). |
| `internal/note/decay.go` (new) | `Decay(n *Note, now time.Time) float64` — `max(decayFloor, 0.5^(age/halfLife))`; `1.0` when unset/`none`/unparseable (unparseable additionally surfaces via doctor, never via ranking). Constants `DecayFloor = 0.30`, `FadedThreshold = 0.5` (spec open Q1 — constants in one place). `Faded(n, now) bool`. |
| `internal/recall/recall.go` | `RankProject` gains a `now time.Time` parameter (or an `Opts{Now}` struct — pick one, update all call sites: CLI recall, MCP recall, `hookBashErrorRecall`, TS-facing paths go through these). Apply `f *= note.Decay(...)` beside the existing `f *= expiredPenalty`. **`Confidence` stays pre-penalty** (documented invariant). Result gains `Faded bool` + `DecayFactor float64` (JSON only). |
| `internal/note/tier.go` | `TierIndex` already takes `now`. Faded non-pinned notes → `rollupNote` even when file-recent (spec §3.3); pinned notes never leave the pinned tier but carry the faded flag through to stubs. Keep the pinned+recent+older == total invariant. |
| `internal/cli` | `nt touch <id…>` (sets `reviewed:`; journaled via the note undo journal; `--expect-mtime`). Flags `--half-life`/`--reviewed` on `nt note` and `nt edit` (+ `--clear-*`), mirroring the `valid_from` pattern in `commands.go`/`maintenance.go`. `nt report`: faded section (most-faded first, capped with count). `nt doctor`: warn on `0d`, garbage durations, future `reviewed`. Stub renderers: `~faded Nd` chip. |
| `internal/mcp` | `nt_note`/`nt_note_edit` args `half_life`, `reviewed` (+ clears). New tool **`nt_touch`** `{id, expect_mtime?}`. Read paths (`nt_get`, `nt_index`, `nt_search`, `nt_recall`) add `faded`/`decay` fields — additive JSON only. |

**Tests** — duration parsing table; decay math with injected `now`
(`determinism_test.go` must stay green — `now` is a parameter, never sampled
in ranking); tier invariant under fade-rollup; byte-identical round-trip for
stores without the new keys (and for old-binary `Extra` passthrough);
`nt touch` concurrency (expect-mtime refusal, undo restores);
`strictargs_test.go` + `coherence_test.go` entries for `nt_touch`;
**eval**: record 50-query-corpus baseline before/after with no `half_life`
set (must be identical — decay is default-inert), plus a decayed-store run
documented in the eval harness README.

**Integrations** — Pi: add `touch` to `NT_WRITE_COMMAND` and `nt_touch` to
`NT_WRITE_TOOL` (A.1.2). Skill/prompt text: one paragraph in
`.claude/skills/nt/SKILL.md` + Pi `AGENTS.md` ("volatile facts get
`half_life`; re-confirm with `nt_touch`, don't re-create"); `nt-distill` and
Pi `prompts/distill.md` gain "faded" as a review lens.

### B.2 Phase 2 — decision log + `nt history`

**Go core**

| File | Change |
|---|---|
| `internal/note/decisions.go` (new) | `AppendDecision(n, date, text)` — find/create exact `## Decisions` heading, prepend `- YYYY-MM-DD: text`; refuse newline/frontmatter-delimiter text (share the hostile-input stance with `invalidFrontmatterLine`); `DecisionStats(n) (count int, latest string)` for stubs. Clone-before-mutate (the round-1 cache-poisoning lesson: never mutate the cache-shared pointer pre-Save). |
| `internal/cli` | `nt decide <id> "text"` (journaled, `--expect-mtime`). `nt history <id>` — `git log --follow [--oneline|-p] -- notes/<rel>` via the existing git shell-out path (`nt sync`'s); clear "run `nt git-init`" error when not a repo; `--since`. |
| `internal/mcp` | **`nt_decide`** `{id, text, expect_mtime?}` (spec open Q3 resolved for the plan: separate tool, matching `nt_relink` precedent of small single-purpose tools; revisit if catalog token pressure bites). **`nt_history`** `{id, patch?, since?}` — truncate large `-p` output with an agent-visible marker. `nt_get`/`nt show --json`: `decisions` count + latest date. |
| seams | `nt distill` merge approval → `AppendDecision(keeper, "absorbed [[loser]] (distill)")`; `markSuperseded` → decision line on the **new** note (`supersedes [[old]]`). Both inside the already-approved operation. |

**Tests** — section create/prepend/idempotent-heading; hostile input;
undo restores the pre-append body; `--follow` across `nt mv`; non-git-store
error text; distill/supersede stamps fire only on approval; strictargs +
coherence for both tools.

**Integrations** — Pi: add `decide` / `nt_decide` to both regexes.
`nt-learn` SKILL + Pi `prompts/learn.md`: when a harvest **changes a
conclusion** on an existing note, the proposal includes the decision line
(agents record *why*, approval-gated as ever). `nt` SKILL + `AGENTS.md`:
"decisions change notes; `nt_decide` records the why; `nt_history` when you
need to know how a note got this way."

### B.3 Phase 3 — `if_exists` steering + recall escalation (land first)

**Go core**

| File | Change |
|---|---|
| `internal/mcp/mcp.go` (note handler) | `if_exists: "create"\|"return"\|"error"` (default `"create"`). Exact match — slug or case-insensitive title, folder-scoped when `folder`/`kind` given, store-wide otherwise; `note.Active()` only (archived/superseded never match — spec §4.1). `"return"`: write nothing; respond `matched:true` + id/rel/description/tags/`mtime` token + the §4.1 steer text. Runs *before* `FindSimilar`; the `similar` flow is untouched for `"create"`. |
| `internal/cli/commands.go` | `nt note --if-exists return\|error` with the same semantics (CLI's fuzzy near-dupe refusal still applies afterwards for `create`). |
| `internal/recall` + `internal/mcp` | Escalation: when top `Tier` < medium or zero results, add `escalate: {reason, try: [nt_search{q, include_archived:true}, nt_index{folder}]}` (spec §6); CLI prints the one-liner. Add `include_archived` to `nt_search`/`nt search` if absent. |

**Tests** — match matrix (slug/title-case/folder-scope/alias-negative for
now — spec open Q4 stays open); archived/superseded exclusion;
`"return"` writes nothing (store-hash unchanged); escalation triggers on the
tier boundary and never on strong results; strictargs/coherence.

**Integrations** — the highest-leverage prompt change in the whole plan:
- `.claude/skills/nt/SKILL.md` + `nt-learn` step 4, Pi `AGENTS.md` +
  `prompts/learn.md`: capture flow becomes *"`nt_note` with
  `if_exists:"return"`; on `matched:true`, `nt_note_edit` (with
  `expect_mtime`) + `nt_decide` if a conclusion changed"* — replacing the
  current "always creates; consolidate afterwards via `similar`" guidance.
- Escalation needs no integration code: the hint rides inside the tool
  result both runtimes already display. Prompts add one line: "if recall
  returns `escalate`, run the suggested `nt_search` before concluding
  nothing is recorded."
- Pi regex note: `nt_note` is already in `NT_WRITE_TOOL`; a `"return"` hit
  that writes nothing still invalidates the compile cache — harmless (one
  extra `store-hash` exec, cache re-validates by hash).

### B.4 Phase 4 — docs, plugin release, drift closure

README + SPEC.md sections (`half_life`/`reviewed` in §5's frontmatter list,
`nt touch|decide|history` in §7.3's command table, config additions);
`docs/claude-integration.md` worked example (capture → touch → decide →
history round-trip); `.claude-plugin/plugin.json` version bump 0.1.0 → 0.2.0
and marketplace description refresh; regenerate/reinstall embedded Pi assets
and re-verify `nt doctor --integrations` reports no drift; `tygo.yaml` type
regeneration if the web frontend picks up `faded`/`decisions` chips (web/TUI
rendering is a fast-follow, not part of this plan's acceptance).

---

## Part C — Rollout matrix (what ships where)

| Change | Go binary | Pi files | Claude files |
|---|---|---|---|
| Decay ranking + flags | `note`, `recall`, `tier`, `cli`, `mcp` | regexes (`touch`) | — (rides MCP) |
| `nt_touch` / `nt_decide` / `nt_history` | `cli`, `mcp` | auto via bridge + regexes (`decide`) | auto via MCP |
| `if_exists` | `mcp`, `cli` | — | — |
| Escalation hint | `recall`, `mcp`, `cli` | — | — |
| Conventions & workflow | — | `AGENTS.md`, `prompts/learn.md`, `prompts/distill.md`, bundled `skills/nt/SKILL.md`, `embed.go` re-embed | `.claude/skills/{nt,nt-learn,nt-distill}/SKILL.md`, `plugin.json` bump |

The Go column is the single source of behavior; both integration columns are
prompts/regexes only. OpenCode (out of this plan's investigation scope per
the request) needs the same regex-and-prompt pass as Pi — tracked as a
follow-up item, flagged here so it isn't silently dropped.

## Part D — Sequencing, risks, estimates

**Order:** Phase 3 (small, independent, immediately reduces dupes) → 1 → 2 →
4. Each phase merges green on: `go vet`, `golangci-lint`, full test suite,
determinism + round-trip suites, and — for 1 and 3 (ranking-visible) — the
50-query corpus showing no regression with features unset.

**Risks & mitigations**

1. *`RankProject` signature churn* (Phase 1) touches every recall call site
   including `nt hook` — do it as a mechanical preparatory commit (`now`
   threaded, behavior unchanged, corpus byte-identical) before any decay math
   lands.
2. *Prompt drift across three copies of the conventions* (Claude skill, Pi
   bundled skill, Pi AGENTS.md) — update in one commit per phase; `nt doctor
   --integrations` catches installed-copy drift but not repo-internal
   divergence, so the phase checklist includes a manual diff of the two
   SKILL.md copies.
3. *Catalog token budget* (two new tools + new args) — the tool descriptions
   are written token-tight from the start; if the catalog grows past the
   budget the integrations already warn about, fold `nt_decide` into
   `nt_note_edit` per spec open Q3's alternative.
4. *Git dependency of `nt history`* — soft by design (clear error, no
   feature creep); the test matrix includes the no-git path explicitly.

**Rough effort** (one contributor, including tests): Phase 3 ≈ 1–2 days;
Phase 1 ≈ 3–4 days (ranking + eval discipline dominates); Phase 2 ≈ 2–3
days; Phase 4 ≈ 1 day. ~1.5 weeks end-to-end at the repo's existing
verification bar.
