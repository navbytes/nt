# nt memory-integration roadmap (2026-07-08)

This roadmap comes from a **5-agent Opus team** (four independent experts —
OpenCode architecture, Pi architecture, nt core architecture, and nt
integration value/market positioning — plus an Opus judge/coordinator) that
researched nt's current integrations against each runtime's actual
capabilities and against market alternatives (Letta/MemGPT, Mem0, Zep,
Cognee, NousResearch's hermes-agent, Cursor/Windsurf rules, Basic Memory,
Anthropic's native Memory Tool), then synthesized, cross-critiqued, and
finalized a ranked, scored backlog. It's recorded here as a durable artifact —
not everything ranked highly was shippable in one sitting, and this is the
tracked backlog for the rest.

North star (from the team's synthesis): nt's differentiation is **plain-text,
git-diffable, local-first, no vendor lock-in, one backend across three
runtimes** — the roadmap deliberately protects that rather than chasing
feature parity with hosted competitors that trade it away.

## Shipped this pass

Eight items, picked from the top of the ranked list for being both
high-impact and verifiable against nt's actual code (not speculation about
external hook behavior no one could test live):

| Rank | Item | Where |
|------|------|-------|
| 1 | OpenCode hybrid injection default — a guaranteed session-start file baseline (loaded via the stable `instructions` config) plus a live transform layer, deduped so a working hook never double-injects. Fixes a real failure mode: `experimental.chat.system.transform` is reported to silently discard its mutation on some builds ([sst/opencode#17100](https://github.com/sst/opencode/issues/17100)), so the old transform-only default could inject **zero** rules with no signal. | `integrations/opencode/plugins/nt-memory.ts`, `opencode.json`, `internal/cli/opencode_install.go` |
| 2 | Pi bridge self-healing respawn — `NtBridge.ensureAlive()` respawns the `nt mcp` subprocess on the next bridged call if it died (crash, or `session_shutdown` firing on something short of a real end), instead of leaving every `nt_*` tool permanently broken for the rest of the process's life. Concurrent calls share one in-flight respawn; a cooldown bounds retries against a persistently broken `nt`. Verified with a standalone runtime harness (kill-and-respawn, concurrent-dedup, and cooldown-after-repeated-failure scenarios). | `integrations/pi/extensions/nt-memory.ts` |
| 3 (partial) | Note write integrity — fixed a real cache-poisoning bug: `nt_note_edit`, `nt_relink`, and `markSuperseded` mutated the cache-shared `*Note` pointer (documented as read-only) *before* `Save()`; a failed save left the poisoned pointer served from cache indefinitely. Now they mutate a clone and let the cache's own mtime/size staleness check pick up the change. **Deferred:** the optimistic-concurrency half (a `SaveIfUnchanged`/staleness-token API) needs its own design pass — it's a new API surface (an expect-token round-tripped through `nt_get`→`nt_note_edit`), not a same-session addition. | `internal/mcp/mcp.go` |
| 4 | `nt store-hash` — a stable fingerprint over `notes/`'s mtimes+sizes, one primitive shared by items 1 and 5 (and a future item 21, persisted index). | `internal/cli/storehash.go` |
| 5 | Memoize `compile()` in both TS plugins — keyed by `nt store-hash` (byte-identical cached output, not just "don't recompute"), with a TTL backstop and write-signal invalidation (a bridged write tool, or a bash command running `nt note/add/…`). Un-memoized, OpenCode's `experimental.chat.system.transform` firing per model request (every tool round-trip, not once per turn) meant dozens of `nt export` spawns per session and a guaranteed prompt-cache miss on the system block every request. | `integrations/opencode/plugins/nt-memory.ts`, `integrations/pi/extensions/nt-memory.ts` |
| 6 | Claude Code hook parity for the recall loop — `nt hook` now also handles the `Bash` PostToolUse matcher: on a failed command it searches recorded lessons for the command + error tail and, on a match, exits 2 with the lesson(s) on stderr (Claude Code's block+reason contract) so the mistake surfaces on the next turn — the same loop OpenCode/Pi already run, now on nt's largest-audience integration. | `internal/cli/hook_recall.go`, `internal/cli/maintenance.go`, `docs/claude-integration.md` |
| 9 (partial) | Fixed a latent bug in `recall.go`'s synonym-group id generation (`"g"+string(rune('0'+i))` overflows past group index 9 into punctuation once the synonym table passes 9 groups — it already had). Full "load synonyms from a user file" feature deferred. | `internal/recall/recall.go` |
| 11 | Undo journal rotation + tail-read `Peek` — `Append` now compacts the journal to its most recent 500 transactions once it crosses a size threshold, and `Peek` reads backward from EOF instead of loading and splitting the whole file. Safe because undo/redo is single-level (only the journal's last line is ever read or replaced — verified by reading `mutate.applyReversal`); entries below the top are audit trail only. | `internal/undo/undo.go` |

Also shipped in an earlier pass this session (before the 5-agent workflow, in
response to a Hermes-agent comparison): an agent-visible over-budget nudge
when the injected rules+memory block exceeds `NT_INJECT_MAX` (both plugins),
and a "what to skip" bullet in the `/learn` prompts.

## Deferred — the rest of the ranked backlog

Not attempted this pass — either genuinely larger scope, dependent on a
deferred item, or resting on an assumption about external hook behavior that
needs to be verified against a live build before shipping. Roughly in rank
order:

- **3 (rest)** — optimistic-concurrency write guard (`SaveIfUnchanged` +
  staleness error), threaded through `nt_note_edit` and `nt edit`.
- **7** — `nt doctor --integrations`: one command surfacing all three
  runtimes' silent-failure modes (Pi bridge death, OpenCode injection no-op,
  Claude Code hook wiring) plus config/asset-drift checks.
- **8** — an `nt memory-serve` adapter implementing Anthropic's Memory Tool
  protocol (view/create/str_replace/insert/delete/rename) against nt's notes,
  positioning nt as the durable backend behind it. Scoped-down reach note from
  the judge: this reaches developers who use the memory beta *and* choose nt
  for their storage handler, not "every Claude API user."
- **9 (rest)** — user-extensible synonym vocabulary loaded from
  `$NT_DIR/synonyms.txt`, merged over the in-code defaults.
- **10** — bring note body edits into the undo/history model (currently
  undo only covers tasks). Depends on item 11 (done) landing first; a note
  before-image is much larger than a task line, so this should store a diff,
  not a full body.
- **12** — publish nt to plugin/MCP registries (Claude Code marketplace,
  OpenCode plugin registry, public MCP directories) — the one gap the team
  flagged as pure distribution, zero code.
- **13** — opt-in local embedding recall as a parallel candidate *source*
  (unioned with lexical candidates, never a re-ranker of them — re-ranking
  can't recover a paraphrase the lexical scorer never surfaced), gated behind
  an eval proving lift over the synonym-table baseline.
- **14** — OpenCode `chat.message` + `tool.execute.before` proactive recall
  (push, not pull) — downgraded in the final pass because `chat.message`
  content is also reported to be droppable on some builds, so it needs to be
  gated behind the same kind of build-detection as item 1, not shipped
  unconditionally.
- **15** — human-gated consolidation pass (`nt distill` / a `/learn` merge
  mode) — clusters near-duplicate lessons/notes and proposes merges, no silent
  auto-delete.
- **16** — optional `valid_from`/`valid_until` frontmatter so a superseded
  fact can be down-ranked/flagged automatically (Zep's idea, no graph DB).
- **17** — a read-only `nt_doctor` MCP tool so an MCP-only agent can see its
  own store's hygiene (near-dups, dangling links, reclaimable weight).
- **18** — a documented git-native shared-team-memory pattern + a thin
  `nt sync` wrapper, reusing `doctor`'s existing duplicate-id merge
  reconciliation.
- **19** — steering hints (`promptGuidelines`/`promptSnippet`) on the Pi
  bridge's key tools — first needs confirming Pi's `registerTool` actually
  exposes those fields in the installed version.
- **20** — nt-aware subagent definitions for OpenCode/Pi subagents, which
  inherit the bridged `nt_*` tools but not the plugin's injection/recall loop
  (that runs on the top-level session only).
- **21** — a persisted note-index sidecar for stores past ~10k notes (every
  MCP read currently stats every note file); consumes item 4's store-hash for
  invalidation.
- **22** — Pi polish bundle: move the idle nudge off "first `agent_end`" to a
  real session end, and make the injection budget context-aware instead of a
  flat char cap.
- **23** — a bulk `nt import` (Obsidian vault / JSON round-trip) — `nt export`
  has no inverse today.

## Explicitly out of scope (the team's `outOfScope`, with reasoning)

- A hosted/cloud multi-user memory **server** (the Letta/Mem0/Zep model) —
  conflicts head-on with nt's no-service local-first posture. The git-native
  shared-team-memory pattern (item 18) is the in-philosophy answer.
- Native mobile app / always-reachable hosted access — nt's web viewer is a
  deliberate localhost PWA; ceded intentionally (see the README's "when nt is
  not for you").
- A full temporal knowledge-graph database (Graphiti/Neo4j-style) — adds a
  heavyweight external dependency and abandons the plain-file substrate.
  Lightweight validity-window frontmatter (item 16) is the in-philosophy
  answer.
- Automatic silent LLM-driven consolidation/deletion (Mem0's unattended
  ADD/UPDATE/DELETE) — violates nt's approval-gate ethos. Human-gated distill
  (item 15) is the answer.
- Embeddings/a vector DB as a **hard dependency or source of truth** — only
  permitted as an opt-in, git-ignored, deletable derived cache that degrades
  to lexical when absent (item 13); markdown stays authoritative.
- Embeddings used *only* as a re-ranker of the lexical top-N — rejected as
  architecturally unsound (can't recover a paraphrase the lexical scorer
  never surfaced in the first place). If embeddings ship, they're a parallel
  candidate source unioned before ranking.
- A graph/ontology ETL ingestion pipeline (Cognee-style) — enterprise-scale
  scope mismatch for a personal-scale single-binary tool.
- A lock/server around note writes to solve concurrency — best-effort
  optimistic mtime/hash staleness detection (item 3, deferred half) is the
  answer; the CLI/MCP path is multi-process, so this is honestly "catches the
  dominant real case," not a hard guarantee.
- A guessed `session_shutdown`-reason allowlist for the Pi teardown guard, or
  building the OpenCode injection fix on the unverified assumption that
  `system.transform` is discarded on *every* build — both would need to be
  empirically confirmed first. What shipped instead (item 1's file baseline,
  item 2's enum-independent respawn) works regardless of the answer.

## Market comparison (from the team's research)

| Alternative | What they do better | nt's response |
|---|---|---|
| Anthropic Memory Tool | First-party, model self-edits `/memories` via the Messages API | Adapt: `nt memory-serve` (item 8, deferred) as the storage handler behind it — reaches app-builders who adopt the beta *and* pick nt, not every API user |
| Mem0 / OpenMemory | Automatic LLM-driven consolidation over vector nearest-neighbors | Adapt: opt-in embeddings as a candidate source (item 13) + human-gated distill (item 15), deliberately not silent auto-delete |
| Zep / Graphiti | Temporal knowledge graph, facts have validity windows | Adapt: `valid_from`/`valid_until` frontmatter (item 16), no graph DB |
| Letta / MemGPT | Self-editing core memory + archival memory on Postgres+pgvector, hosted multi-user | Adapt retrieval quality (item 13) and consolidation (item 15); deliberately skip the hosted server — local-first is the point |
| Cognee | Graph-native ETL with ontology grounding, GraphRAG over enterprise sources | Skip — enterprise scope mismatch |
| Basic Memory | nt's closest philosophical twin — local-first markdown KB from AI conversations, already in MCP directories | nt already exceeds it (unified task model, lessons-first ranked recall, three-runtime integrations); close its distribution lead (item 12) |
| Claude native chat memory | Auto-distilled user profile, zero setup, all Claude surfaces | Complement, not compete — it's a non-searchable profile; nt is the searchable, git-diffable, cross-tool durable store |

---

*Generated from a 5-agent Opus research+synthesis workflow (four independent
experts + a judge/coordinator, with a cross-critique round before
finalizing). See git history for the individual agent reports if deeper
rationale on any item is needed.*
