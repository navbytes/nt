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

## Shipped in round 2

Seven more items off the ranked list, continuing straight from round 1's
merge — same bar (verifiable against nt's own code, full test coverage,
clean build/vet/lint):

| Rank | Item | Where |
|------|------|-------|
| 3 (rest) | Optimistic-concurrency write guard — `Note.MTimeToken()`/`SaveIfUnchanged(expect)` refuse a save whose on-disk mtime moved since the caller last read it, instead of silently overwriting a concurrent edit. Threaded through `nt_note_edit` (`expect_mtime`) and `nt edit --expect-mtime`; `nt_get`/`nt show --json` hand back the token to round-trip. Best-effort (multi-process, so a residual stat-then-rename race remains) — the same honesty the existing `flock` docs already give the task side. | `internal/note/note.go`, `internal/mcp/mcp.go`, `internal/cli/maintenance.go` |
| 10 | Note body edits joined the undo/history model — previously `nt undo`/`nt redo` only ever touched tasks; a note edit outside the temp-file `$EDITOR` path had no revert. A parallel single-level journal (`internal/note/undo.go`, mirroring the task journal's append/compact/peek design) records a before-image per edit; `nt undo`/`nt redo` now peek BOTH journals and act on whichever is more recent, task or note. The old refusal (`noteEditedSinceLastTxn`) is gone — replaced by an actual revert. | `internal/note/undo.go`, `internal/cli/maintenance.go`, `internal/mcp/mcp.go` |
| 16 | `valid_from`/`valid_until` frontmatter (Zep's idea, no graph DB) — a note can be "true as of" or "true until" a date/time without a full supersede. An expired note is never hidden, just down-ranked in `nt_recall`/`nt recall` (`expiredPenalty`) and flagged `expired`/`notYetValid` everywhere a note surfaces (`nt_get`, `nt_index`, `nt_search`, `nt show --json`). Set via `nt note --valid-from/--valid-until`, `nt edit --valid-from/--valid-until` (`--clear-*` to unset), or the matching `nt_note`/`nt_note_edit` MCP args. | `internal/note/note.go`, `internal/recall/recall.go`, `internal/mcp/mcp.go`, `internal/cli/commands.go`, `internal/cli/maintenance.go` |
| 17 | Read-only `nt_doctor` MCP tool — an MCP-only agent (no CLI access) can now see its own store's hygiene: dangling `[[links]]`, task-file problems (duplicate/missing ids, dependency warnings), and notes past `valid_until`. Never writes — task-file fixes stay behind the CLI's `nt doctor` on purpose, so a read can't silently rewrite `tasks.txt`. | `internal/mcp/mcp.go`, `internal/mcp/tools.go` |
| 9 (rest) | User-extensible synonym vocabulary — `$NT_DIR/synonyms.txt` (one group per line, `#` comments) merges over the in-code table at ranking time. A word already in a built-in group extends that group; an all-new line mints its own. Picks up edits with no restart (CLI: new process anyway; MCP: reloaded before every `nt_recall`). | `internal/recall/recall.go` |
| 23 | `nt import` — the inverse `nt export` never had. Round-trips `nt export --format json`'s output into a fresh store, or bulk-loads a folder of markdown files (an Obsidian vault, since nt's note format already reads Obsidian's frontmatter conventions). Always mints fresh ids; skips a title/tag near-duplicate of an existing note unless `--force`; `--dry-run` reports without writing. | `internal/cli/import.go` |
| 18 | Git-native shared-team-memory pattern + `nt sync` — documented the existing `nt git-init` union-merge + `nt doctor` reconciliation as a full pattern (SPEC §6.4), and added the thin wrapper: commit local edits, `git pull --no-rebase` (pinned so it doesn't depend on the caller's global git config), `nt doctor` to reconcile any union-merge duplicate-ULID lines and commit that, `git push`. A genuine same-note conflict stops the sequence with ordinary git conflict markers to resolve by hand. Verified end-to-end with a bare-remote + two-clone integration test. | `internal/cli/maintenance.go` |

## Shipped in round 3

Before this round, an independent model (Fable 5, no prior context on this
session) reviewed all ten remaining backlog items against nt's actual code —
not just the roadmap's description — and gave each a DO/SKIP/REWORK verdict.
Its cross-cutting finding: the team's own verification bar ("verifiable
against nt's actual code, not speculation about external hook behavior no
one could test live") cleanly sorts the list — the three items below passed
it today; the rest structurally can't until someone stands up live
Pi/OpenCode test rigs, or lack a demonstrated need (see Deferred below).

| Rank | Item | Where |
|------|------|-------|
| 7 | `nt doctor --integrations` — rescoped from the original "surface Pi bridge death / OpenCode injection no-op" framing (a CLI command can't observe another process's in-memory state) to static wiring/drift/staleness checks nt's own install code already has the oracle for: MCP registration + hook matchers (Claude Code), permission/instructions/asset-drift (OpenCode), asset-drift (Pi) — all read-only, reusing the exact assumptions `nt mcp/opencode/pi install` already commit to. | `internal/cli/integrations_doctor.go` |
| 15 | `nt distill` (human-gated consolidation) — every primitive it composes (`note.FindSimilar`, doctor's near-dup lint, `superseded_by`, the round-2 note-undo journal as a safety net) already shipped; this adds the missing batch flow: `nt distill`/`nt_distill` list EVERY near-duplicate pair uncapped (doctor's lint samples 5), and a `/distill` prompt per runtime walks each pair to approval before merging or tagging a deliberate fork `distinct` — nothing merges without approval. | `internal/note/dedup.go`, `internal/cli/distill.go`, `internal/mcp/mcp.go`, `integrations/{opencode,pi}/…/distill.md` |
| 22 (half) | Pi idle-nudge threshold — the nudge fired on the *first* tool-using turn with no nt write (premature, trains users to ignore it); fixed with `NT_IDLE_NUDGE_THRESHOLD` (default 3 quiet turns), not the originally-proposed "real session end," which rests on the same unconfirmed `session_shutdown` semantics the extension's own code already flags as unverified. Verified with a standalone runtime harness (threshold timing, write-anywhere suppression, per-session reset). | `integrations/pi/extensions/nt-memory.ts` |

## Deferred backlog — triaged by an independent advisor (Fable 5)

**Dropped from the backlog entirely** (advisor verdict: SKIP, not "later"):

- **19** — Pi steering hints (`promptGuidelines`/`promptSnippet`) — rests on
  an unverified Pi API, and the MCP tool catalog already carries steering
  guidance token-trimmed on purpose; hints would spend back what that trim
  saved for an unmeasured lift.
- **20** — nt-aware subagent definitions — ships opinionated files built on
  an unverified inheritance assumption (do OpenCode/Pi subagents get the
  bridged tools but not the injection loop?) for a need no user has
  expressed. If real, the honest zero-risk fix is a paragraph in the
  integration READMEs, not a shipped asset.
- **21** — persisted note-index sidecar — the problem statement overstates
  the cost (stats are cheap; the MCP server's mtime cache already skips
  re-parsing unchanged notes), and a derived on-disk cache adds real
  complexity (schema versioning, corruption handling, invalidation) for a
  store size nt has no evidence anyone has hit. Design kept, item dropped.

**Parked behind an explicit precondition** (revisit if the precondition is
met, don't schedule otherwise):

- **8** — `nt memory-serve` (Anthropic Memory Tool adapter) — re-checked
  post-round-3 (2026-07-08): the Memory Tool is now **GA, no beta header**
  (it was beta when originally scoped), which cuts the other way from what
  you'd expect — Anthropic's own SDKs (Python/TypeScript) now ship a **free
  local-filesystem memory backend** (`BetaLocalFilesystemMemoryTool`) out of
  the box, so "just store memory files locally" no longer needs a
  third-party adapter at all. Combined with the pre-existing impedance
  mismatch (the protocol's exact line-numbered `view`/`insert`/`str_replace`
  contract — 1-indexed, 6-char right-aligned line numbers, precise error
  strings — doesn't map onto frontmatter-bearing notes without either
  exposing frontmatter to the model or breaking line-number bookkeeping),
  this **hardens the skip**, not just holds it. Still parked on "a user
  actually asks"; if ever built, a raw-files `memories/` subtree, not notes.
- **13** — opt-in local embedding recall — parked on "the paraphrase eval
  shows the synonym-table baseline (now user-extensible, round 2) actually
  failing on a real store." No evidence of that yet, and nt's go.mod is
  CGO-free/single-binary on purpose — bundling an embedding model or
  depending on an external service both cut against that. The cheap,
  philosophy-neutral first move (build only if curious) is extending
  `recall_precision_test.go` into a paraphrase corpus.
  - **The corpus now exists** (`internal/recall/paraphrase_corpus_test.go`,
    2026-07-28): 12 notes, 8 queries sharing no verbatim content word with
    their target, an asserted HIT@1 floor, and a guard that a glossary note
    never takes #1. It immediately found the precision floor returning
    *nothing* for 3 of 8 paraphrased queries; softening it took HIT@1 from
    5/8 to 8/8.
  - **Length normalization ruled out (honest eval confirms); real blocker
    identified — do not re-attempt length normalization without new
    evidence.** A prior field test reported HIT@1 falling 65% → 41% as a
    store grew, diagnosing length as the cause. Tested against a user's
    actual store under controlled conditions (22-query corpus built by
    enforced information asymmetry: one agent wrote scenarios from real notes
    with distinctive vocabulary stripped; a second agent, unseen notes/titles,
    wrote queries from scenarios alone):
    - **Growth-driven degradation is real, and the curve below is corrected.**
      The harness originally counted `__tasks__/` reserved notes toward each
      corpus size and toward IDF's document-frequency `n`, even though
      `RankProject` never scores them as candidates — inflating every labeled
      size by ~10% (of 218 "active" notes, 23 were reserved). Fixed
      (`note.Reserved()` now filtered alongside `note.Active()`) and
      re-measured: HIT@1 by corpus size — smooth, in fact fully monotone,
      decline: size 22 → 18/22 (targets only, no distractors — every
      competitor at this size is another correct answer, a different regime
      from the rest of the curve), size 40 → 16/22, size 100 → 15/22, size
      160 → 13/22, size 195 (whole store; the true scored size, not 218) →
      12/22. (At 22 queries per hit ≈ 4.5pp.) The reserved-note fix changed
      the counts at 100 and 160 and relabeled "218" to "195," but the
      degradation conclusion is unchanged — if anything the corrected curve
      is cleaner (no tail wobble).
    - **Length normalization is flat — swept `b` ∈ {0, 0.25, 0.5, 0.75, 1.0}:**
      HIT@1 was 16/22 at size 40 and 12/22 at full store at **every** value,
      with the paraphrase corpus staying 8/8 throughout. The knob moved scores
      (computed norms ranged 0.27–1.40) but never rankings — length is not
      the operative variable. This *refines* the earlier verdict: length does
      not cause the harm.
    - **Unresolved: this result disagrees with the earlier field test's own
      `b` sweep.** That sweep (`b` ∈ {0, 0.1, 0.2, 0.3, 0.4, 0.55, 0.75, 1.0})
      found **every** non-zero `b` — including the same 0.75 and 1.0 tested
      here — dropped the paraphrase corpus 8/8 → 7/8. This session's sweep,
      over the same 0.75/1.0 values, found 8/8 throughout. Both cannot be
      literally true at once; this was not re-reconciled and is left open. Do
      not read the "partial mitigations" bullet below as an explanation —
      combining stopwords with `b=0.75` is a different, later experiment that
      also produced an 8/8 → 7/8 drop, and it is not established that it is
      the same effect the earlier session hit with `b` alone.
    - **The actual mechanism:** Closed-class function words are scored as
      topical terms. Over the store's terse notes, IDF cannot distinguish
      "uninformative in English" from "rare in store": `should` (df=5) scores
      IDF 3.69, above `swift` (3.12) in an iOS-heavy store; a function word in
      a title then earns full strong-bag weight. Growth trigger: target score
      stays fixed; max distractor score rises sharply. Measured size 25 →
      218 (this instrumented run predates the reserved-note fix and was not
      rerun — see provenance note below): **target +19.7%, best distractor
      +106.9%**. Function-word share of winning note's score: **0.02 on a
      hit, 0.44 on a miss.**
    - **Hard ceiling: lexical retrieval cannot cross it by reweighting.**
      Targets sharing ≤2 query concepts scored 0/6; those sharing ≥3 scored
      12/16.
    - **Partial mitigations bounded.** Expanded stopwords gained +1–2
      mid-range precision but did not fix growth; combined with `b=0.75`
      reached 21/22 at size 40 but collapsed by size 100 and broke the
      paraphrase corpus 8/8 → 7/8. The precondition holds: embeddings/semantic
      retrieval cannot land without semantic-distance quality, which lexical
      scoring architecturally cannot provide.
    - **Evaluation methodology.** This session's honest-corpus trial used the
      information-asymmetry protocol (scenario-writer unsees notes+titles;
      query-writer unsees source) to isolate corpus distortion from scorer
      weaknesses — that discipline is why these numbers are trustable. The
      earlier session's growth case did **not** use this protocol: its
      distractors were found adversarial by construction only after the fact
      (one contained "the wait budget for writers" against a query of
      "writers exceed the wait budget"). Standing caveat, independent of
      either session's results: planted near-duplicates must not be used to
      tune the ranker.
    - **Provenance.** The corpus-size HIT@1 curve above is exactly what the
      committed harness, `internal/recall/realstore_eval_test.go`
      (`NT_EVAL_STORE`/`NT_EVAL_CORPUS`/`NT_EVAL_SIZES` env vars; run with
      `go test ./internal/recall/ -run RealStore -v`), regenerates against a
      real store and corpus. The `b` sweep, the IDF and function-word-share
      numbers, and the score-growth percentages above were one-off
      instrumented runs during this and the earlier session, not reproducible
      from this branch as committed. No real note content is committed
      (stores hold private client data); anyone can re-run the harness
      against their own store.
    - **Re-run post-consolidation (2026-07-29): it now measurably helps —
      the flat verdict above was correctly measured, on a store that
      happened to have no length variance to normalize against.** Item 15's
      consolidation ran between the flat sweep above and this re-run
      (193 → 142 notes), and it changed the store's *shape*, not just its
      size: the flat-sweep store had roughly 75% empty-body notes; **0 of
      142 notes now have an empty body** (median body 866 chars), and the
      weak-bag length band went from ~5x to 166x. Length normalization has
      nothing to divide by when most notes carry no body — that store was a
      genuine negative result, just not a general one. Re-swept `b` ∈ {0,
      0.25, 0.5, 0.75, 1.0} on the changed store, HIT@1 (of 22 queries):
      size 22 → 17/18/18/17/15, size 40 → 14/15/16/18/17, size 100 →
      13/12/13/13/13, size 142 (whole store) → 12/12/**13**/**13**/10 — a
      +1-hit (+4.5pp) improvement at `b` ∈ {0.5, 0.75} over `b=0`, with the
      paraphrase corpus holding 8/8 at every value. Repeated measures (5
      distractor draws × 2 sizes) at `b=0.5`: positive in 7/10, zero in 3,
      **negative in none** (sign test p≈0.008 — not noise). Mechanism,
      confirmed per query: a short target note overtakes a 1.8–2.0x longer
      generalist note competing on the same concepts.
    - **This also settles the earlier unresolved contradiction — it was
      per-bag vs. combined-bag, not two measurements disagreeing with
      themselves.** The very first field test's `b` sweep divided by ONE
      combined strong+weak divisor and found every non-zero `b` dropped the
      paraphrase corpus 8/8 → 7/8. The flat-sweep bullet above (this item's
      honest-corpus re-test) reported 8/8 throughout at the same `b` values
      and was left unreconciled. Replaying both modes against the
      post-consolidation store settles it: **combined**-bag reproduces the
      8/8 → 7/8 drop (including at `b=0.1`); **per-bag** (each bag divided
      by its own average) reproduces 8/8 throughout. The flat-sweep bullet's
      8/8 identifies it as per-bag all along — so the honest comparison is
      per-bag-then (inert, no length variance) vs. per-bag-now (helps, after
      consolidation created the variance). Neither session mismeasured;
      they normalized different scopes. Per-bag is also the structurally
      correct default, independent of which number is better: `strong`
      (title+tags+description) is a bounded 240-char field with low
      variance, `weak` (body) is unbounded, and a combined divisor lets a
      long body dilute a note's TITLE match — the bias BM25F's per-field
      normalization exists specifically to avoid.
    - **Shipped, default OFF, behind `NT_LENNORM_B`** — per-bag only;
      combined mode was measured and rejected, see above. Default-off is
      deliberate, not a hedge: the effect is real but modest (+4.5pp at the
      whole store, 1 hit out of 22), it costs some rank to long, thorough
      notes, and a 22-query corpus is too thin to trust flipping a
      production default. `b=0` (the shipped default) is proven
      byte-identical to the pre-normalization scorer
      (`TestLenNormZeroIsIdentical` in `internal/recall/lennorm_test.go`).
      Revisit the default once a real-store corpus exists large enough to
      stand on a single measurement instead of a repeated-measures sign
      test.
    - **Honest caveat, still open: "no length variance ⇒ inert" is
      plausible, not proven.** A bodies-stripped control arm (removing body
      content outright, rather than relying on the store's own empty notes)
      was *also* not inert — so the flat-sweep verdict's explanation may be
      incomplete. The real threshold at which normalization starts to bite
      sits somewhere between a 5x and a 26x weak-bag length band, not
      cleanly at "zero variance." Left for whoever next revisits the
      default.
    - **Reusable lesson: the consolidation work looked like it failed on
      its own terms, but it created the length variance this needed.** Item
      15's consolidation (193 → 142 notes) was measured against flat HIT@1
      and looked like it made no difference to recall quality — but filling
      the empty bodies is exactly what makes length normalization non-inert.
      A technique can be correctly measured as useless and later become
      useful because the *data* changed, not the code; "inert" is a
      property of the corpus it was measured against, not a permanent
      verdict on the technique.
    - **Bigger-corpus findings (50 queries, 2026-07-29): length normalization
      does NOT survive the extended corpus.** The 22-query corpus was too thin
      to decide either of the open questions; in one case, it pointed the wrong
      way. The earlier +1 hit at full store (12/22 → 13/22 at `b=0.5`) was
      noise that a 50-query corpus (22 original + 28 new targets, 21 of them
      merged keepers, deliberately oversampling the under-represented population)
      flattens to no gain at any size:
      - **Length-norm sweep on 50-query corpus:**

        | b | size 60 | size 100 | whole store (145) |
        |---|---|---|---|
        | 0 | 34/50 | 31/50 | 32/50 |
        | 0.25 | 37/50 | 34/50 | 32/50 |
        | 0.5 | 38/50 | 34/50 | 32/50 |
        | 0.75 | 34/50 | 33/50 | 31/50 |
        | 1.0 | 34/50 | 31/50 | 29/50 |

      - Paraphrase corpus stays 8/8 at `b ∈ {0, 0.5, 1.0}` throughout.
      - Real, repeatable gains mid-corpus (+4 at size 60, +3 at 100), exactly
        flat at full store where the smaller corpus had shown +1. **`NT_LENNORM_B`
        remains default-off; the reason is now measured rather than precautionary.**
        The setting is safe opt-in (never worse at any size), but does not earn a
        default on a bigger corpus. This closes the "pending a bigger corpus"
        caveat: the bigger corpus answered it.
    - **`triggers:` proposal is retired, on inverted evidence.** The proposed
      `triggers:` frontmatter list (multiple retrieval surfaces per note) rested
      on **trigger collapse**: merging N notes collapses N descriptions into one,
      so a merged note should retrieve *worse* for the facets it absorbed. This
      could not be tested before — the 22-query corpus had only 6 merged keepers.
      With 21 merged-keeper targets in the 50-query corpus, each query
      deliberately written for the keeper's weakest absorbed facet (the vocabulary
      least present in its current title/description), HIT@1 on the full store
      splits exactly opposite to the hypothesis:
      - **Merged keepers: 20/28 (71.4%)**
      - **Atomic notes: 12/22 (54.5%)**
      - **Fisher exact two-sided p = 0.249** (not statistically significant, but
        the direction is the one the hypothesis forbids)
      - Merged notes retrieve at least as well as atomic ones. The failure mode
        observed in merged-keeper misses is different: they lose to *other* merged
        keepers within dense topical clusters (one term-rich note won three
        separate queries that belonged to other notes). This is consistent with
        the earlier concatenation control arm, where the big score movers were
        distractors becoming term-rich generalists (+775 to +1749) while no
        target's score rose.
      - **Consequence: `triggers:` is closed as a design, not held pending proof.**
        There is no evidence of trigger collapse, measured on the population
        designed to expose it. The tribunal that reviewed it had already reduced
        it to "revise" pending exactly this measurement. Record it as closed so a
        future session does not re-propose it without new evidence — and record
        what evidence *would* reopen it: a demonstration that merged notes
        systematically lose queries belonging to their absorbed facets.
    - **Methodological point: a 22-query corpus was too thin to decide either
      question.** The length-norm +1 at full store was noise. Any future ranking
      change should be judged on the 50-query corpus, where one hit is now 2pp
      rather than 4.5pp.
- **14** — OpenCode `chat.message` + `tool.execute.before` proactive recall
  — parked on "a live OpenCode build matrix to test against," the same
  precondition item 1 needed before it could ship safely. Also lower-value
  than it looks: push-on-failure recall already ships in all three
  runtimes, and the compaction hook already pushes a re-recall directive —
  the only new capability is recall-on-topic-shift.
- **22 (other half)** — context-aware injection budget — needs Pi to expose
  model/context-window info in `before_agent_start`, unverified; the
  shipped flat-cap behavior already degrades gracefully (truncation +
  agent-visible warning).
- **12** — publish to registries — **mostly done** (2026-07-08):
  - **Homebrew**: `navbytes/homebrew-tap` + the PAT secret exist,
    `.goreleaser.yaml` switched from the fully-removed `brews:` to
    `homebrew_casks:` (verified against GoReleaser's own dogfooded config),
    `brew install navbytes/tap/nt` ships from the next tag.
  - **mise**: documented `mise use -g github:navbytes/nt` — works today via
    mise's `github:` backend with zero repo changes, since nt's release
    assets already follow a standard name/version/os/arch pattern. Tried the
    *official* registry (bare `mise use nt`) via
    [jdx/mise#10867](https://github.com/jdx/mise/pull/10867) — withdrawn:
    their registry gates new tools on real popularity (stars/forks/downloads,
    a hard no-appeal bar per their contributing guide), and nt (6 stars,
    ~1 month old) doesn't clear it yet. Resubmit once nt has real traction;
    `github:navbytes/nt` is a full substitute until then.
  - **Claude Code plugin marketplace**: nt is now a self-hosted marketplace
    (`.claude-plugin/marketplace.json` + `plugin.json` at repo root) —
    `/plugin marketplace add navbytes/nt` + `/plugin install nt@navbytes-nt`
    installs the skill and registers the MCP server in one step. Verified
    with `claude plugin validate` and a local install/uninstall round-trip.
    The *central* Anthropic-curated directory (`claude-plugins-official`)
    is a separate, reviewed submission — left for the maintainer.
  - **Still open, external submissions**: MCP directory listings (e.g. the
    official `modelcontextprotocol/registry` — its `server.json` schema only
    accepts npm/PyPI/NuGet/OCI/MCPB package types, not a raw GitHub-release
    binary, so this needs a packaging decision — npm wrapper, a Docker/OCI
    image, or an MCPB bundle — before it's a fit; not attempted without that
    call), and the Claude Code marketplace's central directory. Context7
    (`context7.json` already in the repo) needs the repo submitted at
    context7.com. All left for the maintainer — no more code-side
    prerequisites for Homebrew, mise, or the self-hosted plugin path.

## Explicitly out of scope (the team's `outOfScope`, with reasoning)

- A hosted/cloud multi-user memory **server** (the Letta/Mem0/Zep model) —
  conflicts head-on with nt's no-service local-first posture. The git-native
  shared-team-memory pattern (item 18, shipped round 2) is the in-philosophy
  answer.
- Native mobile app / always-reachable hosted access — nt's web viewer is a
  deliberate localhost PWA; ceded intentionally (see the README's "when nt is
  not for you").
- A full temporal knowledge-graph database (Graphiti/Neo4j-style) — adds a
  heavyweight external dependency and abandons the plain-file substrate.
  Lightweight validity-window frontmatter (item 16, shipped round 2) is the
  in-philosophy answer.
- Automatic silent LLM-driven consolidation/deletion (Mem0's unattended
  ADD/UPDATE/DELETE) — violates nt's approval-gate ethos. Human-gated distill
  (item 15, shipped round 3) is the answer.
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
| Anthropic Memory Tool | First-party, GA on the Messages API, and Anthropic's own SDKs now ship a free local-filesystem backend | Skip (item 8) — the free official backend covers the exact niche `nt memory-serve` would have; not worth building a competing adapter without a user asking |
| Mem0 / OpenMemory | Automatic LLM-driven consolidation over vector nearest-neighbors | Adapt: opt-in embeddings as a candidate source (item 13, deferred) + human-gated distill (item 15, shipped round 3), deliberately not silent auto-delete |
| Zep / Graphiti | Temporal knowledge graph, facts have validity windows | Adapt: `valid_from`/`valid_until` frontmatter (item 16), no graph DB |
| Letta / MemGPT | Self-editing core memory + archival memory on Postgres+pgvector, hosted multi-user | Adapt retrieval quality (item 13, deferred) and consolidation (item 15, shipped round 3); deliberately skip the hosted server — local-first is the point |
| Cognee | Graph-native ETL with ontology grounding, GraphRAG over enterprise sources | Skip — enterprise scope mismatch |
| Basic Memory | nt's closest philosophical twin — local-first markdown KB from AI conversations, already in MCP directories | nt already exceeds it (unified task model, lessons-first ranked recall, three-runtime integrations); close its distribution lead (item 12) |
| Claude native chat memory | Auto-distilled user profile, zero setup, all Claude surfaces | Complement, not compete — it's a non-searchable profile; nt is the searchable, git-diffable, cross-tool durable store |

---

*Generated from a 5-agent Opus research+synthesis workflow (four independent
experts + a judge/coordinator, with a cross-critique round before
finalizing). See git history for the individual agent reports if deeper
rationale on any item is needed.*
