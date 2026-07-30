# Memory dynamics — decay, delta-writes, and decision history

**Status:** open spec / proposal — not yet implemented
**Date:** 2026-07-30
**Author:** Naveen (with Claude research assist)
**Discussion:** feature branch `feat/memory-dynamics-spec`

An open spec for making nt's memory layer *dynamic*: knowledge that fades at a
per-note rate instead of sitting frozen, canonical notes that are edited in
place instead of duplicated, and a lightweight per-note decision history that
lets an agent see how understanding *evolved* — not just its latest state.

The design is inspired by the two architectural mechanisms behind Kimi K3's
~2.5× training-efficiency gain over K2 ([technical
report](https://arxiv.org/pdf/2607.24653)): **Kimi Delta Attention** (a
fixed-size memory updated by an erase-then-write delta rule with learned,
per-channel decay) and **Attention Residuals** (later layers selectively
retrieve from *all* earlier layers' outputs — a queryable version history
across depth — rather than receiving only one accumulated state). Those are
neural-net mechanisms, but each one names a policy that a plain-text memory
store can implement directly. Section 2 gives the mapping.

Everything here honors nt's locked philosophy (SPEC §1–2): plain files, no
database, no index that can drift, no silent LLM-driven consolidation
(human/agent-gated writes only), old information down-ranked and flagged —
**never hidden**.

---

## 1. Problem statement

nt already survives sessions. What it does not yet model is that knowledge
*changes shape over time*:

1. **Everything ages at the same rate — which is to say, not at all.** A note
   recording a config gotcha for a tool version that shipped three upgrades
   ago ranks the same in `nt_recall` today as the day it was written, unless
   someone manually set `valid_until` (a hard cliff that requires knowing the
   expiry date in advance — usually you don't). The index tiers by *recency of
   edit*, which is a proxy for freshness of the file, not relevance of the
   fact.
2. **Agents append instead of editing.** `nt_note` always creates. An agent
   that learned something new about JWT lifetimes writes
   `jwt-token-lifetime-2.md` next to `jwt-token-lifetime.md`; recall then
   surfaces both, including the one that is now wrong. `nt distill` cleans
   this up after the fact (human-gated, correctly), but nothing steers the
   write toward the existing note at capture time.
3. **In-place edits erase history.** The fix for (2) — edit the canonical
   note — has a cost: the superseded claim vanishes from the note. The next
   agent can't see that approach X was already tried and abandoned, so it
   re-proposes X. Git has the full history, but nothing surfaces it at read
   time, and full `git log -p` output is far too heavy to hand an agent by
   default.

These three interlock: you can't ask agents to edit-in-place (2) without
giving readers a history channel (3), and decay (1) is what keeps the
append-only journal/lesson layers from drowning the canonical layer.

## 2. Design inspiration → concrete mechanism

| Kimi K3 mechanism | What it does there | nt translation (this spec) |
|---|---|---|
| KDA delta rule `(I − βkkᵀ)S`: erase the old value at a key before writing the new one | State is *edited*, not appended; no contradictory duplicates accumulate | §4 `if_exists` steering on `nt_note`: route an agent's write to the existing canonical note instead of minting a near-duplicate |
| KDA per-channel learned decay `α ∈ (0,1)` | Stale information fades gradually, at a content-dependent rate; nothing is deleted | §3 `half_life:` frontmatter + smooth down-ranking in recall and index tiering, with a floor — fade, never hide |
| KDA write gate `β` | Not every token deserves a state write | §7 capture-bar guidance in the MCP tool catalog (documentation change, no code) |
| AttnRes: each layer attends over *all* prior layers' outputs | Latest version **plus** queryable version history; selective, not force-fed | §5 `## Decisions` section (cheap always-visible summary) + `nt history` (full git history, fetched on demand) |
| AttnRes block granularity (8 blocks ≈ most of the benefit at O(N) cost) | Chapter-level history recovers nearly all the value of per-edit history | Decision log = one line per *decision*, not per edit; git remains the per-edit layer underneath |
| 3:1 KDA / full-attention hybrid layout | Cheap compressed state most of the time; periodic exact global recall | §6 escalation hint on low-confidence recall: tell the agent *when* to fall back from the index/recall layer to full ripgrep search + history |

Prior art already in nt that this spec deliberately builds on rather than
duplicates: `valid_from`/`valid_until` + `expiredPenalty` (binary validity
cliff — kept; decay is the smooth complement), `superseded_by` + `nt distill`
(after-the-fact consolidation — kept; `if_exists` reduces how often it's
needed), the tiered index (`internal/note/tier.go` — decay plugs into it),
the note undo journal and `SaveIfUnchanged` mtime tokens (the decision-log
appender reuses both), and `nt git-init`/`nt sync` (`nt history` is a read
view over that, not a new storage layer).

## 3. Feature: relevance decay (`half_life:`)

### 3.1 Frontmatter

Two new optional keys, both preserved-if-unknown today (they ride in `Extra`
until implemented, so stores can adopt the convention before the release):

```yaml
half_life: 90d        # relevance half-life; duration: Nd / Nw / Nm / Ny, or "none"
reviewed: 2026-07-30  # last human/agent re-confirmation (YYYY-MM-DD or RFC3339)
```

- `half_life` unset ⇒ **no decay**. Existing stores behave byte-identically.
  `none` is an explicit opt-out that also blocks any future class default.
- `reviewed` is the decay clock's reset point. Age basis =
  `now − max(reviewed, updated, created, file mtime date)` — i.e.
  `ChangedDate()` extended with `reviewed`. Reviewing a note without editing
  it must be possible: confirming a fact is still true is real information.

### 3.2 Decay factor

```
age      = now − ageBasis                    // days, computed with injected now
decay(n) = max(decayFloor, 0.5 ^ (age / halfLife))
```

- `decayFloor = 0.30` (proposal — see Open Questions). Same spirit as
  `expiredPenalty = 0.4`: a fully-faded note is down-weighted, never zeroed.
  Fade, never hide.
- Composition with the validity window: multiplicative,
  `f *= decay(n)` alongside the existing `f *= expiredPenalty`. A note can be
  both expired and faded; the penalties compose because they encode different
  facts (known-invalid vs. probably-stale).
- Like the lesson boost and `expiredPenalty`, decay applies to the **ranking
  score only** — `Confidence` stays pre-boost/pre-penalty, exactly as
  documented in `recall.go`, so a faded note that matches strongly still
  *reports* strong match confidence while ranking lower. The reader sees both
  signals and decides.

### 3.3 Where decay is read

| Surface | Behavior |
|---|---|
| `nt recall` / `nt_recall` | Score multiplier as above; results gain `"faded": true` (JSON) / a `~faded Nd` chip (CLI/TUI) when `decay < fadedThreshold` (proposal: 0.5, i.e. one half-life elapsed) |
| `nt index` / `nt_index` | A faded note is treated as *old* regardless of file recency: excluded from the Recent tier and rolled into the per-folder counts (reusing `rollupNote`, so the pinned+recent+older arithmetic invariant holds). Pinned notes **never** auto-fade out of the pinned tier — standing knowledge is pinned *because* its relevance doesn't decay with age (`tier.go`'s own words) — but a pinned note that carries an explicit `half_life` does show the `~faded` chip as a review nudge. |
| `nt show` / `nt_get` / `nt_search` | Flag only (`faded`, plus `decay` value in JSON). Read paths never reorder or hide. |
| `nt report` (weekly review) | New section: faded notes, sorted most-faded first — the human-gated review pass where the real decision (re-confirm via `reviewed:`, archive, or edit) happens. |
| `nt doctor` | Warns on nonsense (`half_life: 0d`, unparseable durations, `reviewed` in the future) — warn-and-preserve, consistent with how doctor treats other frontmatter. |

### 3.4 Resetting the clock

`nt touch <id…>` (CLI) / `nt_touch` (MCP): sets `reviewed:` to today. Journaled
through the note undo journal; honors `--expect-mtime`/`expect_mtime`. The
TUI review flow (`nt report`) gets a keybind to touch the selected note.

Explicitly **not** in this spec: automatic `reviewed:` bumps when a note is
merely read or recalled. Reading is not confirming — auto-touch would turn
frequently-*retrieved* wrong notes immortal, the exact failure decay exists
to fix. (This is also why decay uses `reviewed`, not access frequency: nt has
no access log, and shouldn't grow one for this.)

### 3.5 Class defaults — deferred

The K3 analogy ("per-channel learned decay") suggests per-folder/per-tag
default half-lives (journal fast, lessons slow, ref never) in `config.toml`.
Deliberately **out of this spec's v1**: it adds a config surface before
there's evidence per-note opt-in is insufficient, and every ranking-visible
default must be judged against the 50-query eval corpus first (see §8).
Recorded here so the follow-up has its design anchor.

## 4. Feature: delta-writes (`if_exists` on note creation)

The KDA delta rule erases the old value at a key before writing the new one.
nt's "key" is the canonical note for a topic; the failure is that `nt_note`
always mints a new file. The fix is **steering, not merging**: no silent
consolidation, per the locked out-of-scope list in the memory roadmap.

### 4.1 Surface

New optional arg on `nt_note` (MCP) and `nt note` (CLI, `--if-exists`):

```
if_exists: "create" | "return" | "error"     // default "create" (today's behavior)
```

Match rule (deterministic, lexical — no similarity scoring in the hot write
path): same slug, or case-insensitive same title, scoped to `--folder` when
given, store-wide otherwise. Archived and superseded notes never match — a
consolidation decision must not resurrect through the side door.

- `"return"` on match: **write nothing.** Respond with the existing note's
  id, rel path, `mtime_token`, description, and tags, plus
  `"matched": true` and a one-line steer:
  *"note exists — edit it via `nt_note_edit` with `expect_mtime`, and record
  a `## Decisions` line if this changes a conclusion (nt_decide)."*
  The agent then makes an ordinary, journaled, optimistic-concurrency-guarded
  edit — every existing safety rail applies unchanged.
- `"error"` on match: hard refusal with the same payload. For scripts that
  want create-only semantics.
- No match: create exactly as today, regardless of mode.

### 4.2 Why not upsert?

An earlier draft had `"upsert"`: apply the body/metadata to the matched note
in one call. Rejected: it would put a write with merge semantics (replace
body? append? which fields win?) behind a single opaque call, bypassing the
`expect_mtime` round-trip that round 2 of the memory roadmap added
specifically so concurrent edits fail loudly. `"return"` keeps the
read-modify-write loop visible to the agent and to the undo journal. This is
the same reasoning that made `nt distill` human-gated: nt steers, the caller
decides.

### 4.3 Integration defaults

The Claude Code skill, OpenCode plugin, and Pi extension prompts change their
capture guidance to pass `if_exists: "return"` by default. That is a
prompt/docs change shipped with this feature, not a behavioral change to the
tools themselves — a bare `nt_note` call keeps today's semantics forever.

## 5. Feature: decision log + `nt history`

AttnRes gives later layers the latest representation *plus* selective access
to earlier ones. The nt translation is two history channels with different
granularity and cost, matching K3's block-granularity finding: a coarse
always-visible summary, and the full per-edit record behind an explicit call.

### 5.1 The `## Decisions` section (coarse channel)

A convention plus an appender — the section is ordinary markdown body, so
every existing read/render/search path works on it with zero changes.

```markdown
## Decisions

- 2026-07-30: switched refresh window 7d → 30d — mobile clients were
  re-authing weekly; see [[auth-incident-0729]]
- 2026-06-05: chose JWT over session cookies (stateless workers)
```

- Exact heading `## Decisions`; one `- YYYY-MM-DD: text` bullet per entry,
  newest first. Wikilinks resolve normally.
- One line per **decision**, not per edit — typo fixes and rewording don't
  earn entries. Chapter granularity: K3 found ~8 block summaries recover most
  of the benefit of full per-layer history at a fraction of the cost; the
  per-edit layer stays in git (§5.2).
- **Appender:** `nt decide <id> "text"` (CLI) / `nt_decide {id, text}` (MCP).
  Creates the section (at end of body) if missing, prepends the dated bullet
  if present. Journaled via the note undo journal; honors
  `expect_mtime`. Refuses text containing a newline or a frontmatter
  delimiter line (same hostile-input stance as `invalidFrontmatterLine`).
- **Automatic entries at consolidation seams** (mechanical provenance stamps,
  not LLM summaries, so they don't violate the no-silent-consolidation rule):
  - `nt distill` merge approval appends to the keeper:
    `- 2026-07-30: absorbed [[<loser-slug>]] (distill)`
  - `markSuperseded` appends to the **new** note:
    `- 2026-07-30: supersedes [[<old-slug>]]`
  Both fire only inside an operation the caller already approved.
- Read surfaces: nothing new required (it's body text). Two cheap niceties:
  `nt show --json`/`nt_get` add `"decisions": N` (count) and the date of the
  latest entry, so an index-level reader can spot an active note without
  fetching the body; `nt search` hits inside the section rank normally.

### 5.2 `nt history <id>` (fine channel)

A read-only view over the git layer that `nt git-init`/`nt sync` already
establish — **no new storage, no shadow log.**

```
nt history <id>              # one line per commit touching the note (git log --follow --oneline -- <rel>)
nt history <id> --patch      # full diffs (git log --follow -p)
nt history <id> --since 30d
```

- MCP: `nt_history {id, patch?: bool, since?: string}` — default is the
  oneline form; `patch` is the explicit escalation, and its output is subject
  to the same size honesty as other large outputs (truncate with an
  agent-visible marker, never silently).
- Store not a git repo → clear error naming the fix: `nt git-init`. Not a
  git-repo requirement creep: the feature simply has nothing to read without
  it, and says so.
- Implementation note: shells out to `git` (already a soft dependency of
  `sync`/`git-init`); never writes; `--follow` so `nt mv` renames don't
  truncate history.

### 5.3 The layered read model (what an agent is told)

```
always loaded:   nt index            — tiered stubs (pinned + recent + counts)
on topic:        nt_recall / nt_get  — canonical note, incl. its ## Decisions
when signaled:   nt_search           — full-text over everything, archived included
                 nt_history          — how this note got to its current state
```

Latest version first, history on demand, selectively — that is AttnRes as a
retrieval policy.

## 6. Feature: recall escalation hint

K3 interleaves 3 cheap KDA layers with 1 full-attention layer: compressed
recall most of the time, exact global lookup at a known cadence. nt's two
layers exist (recall/index vs. ripgrep search over everything); what's
missing is telling the agent *when* to cross.

`nt_recall` already computes `Confidence` and buckets it into a `Tier` word
precisely so agents don't interpret raw scores. Addition: when the top
result's tier is below medium — or there are zero results — the response
includes:

```json
"escalate": {
  "reason": "low_confidence",       // or "no_results"
  "try": [
    {"tool": "nt_search", "args": {"q": "<query>", "include_archived": true}},
    {"tool": "nt_index",  "args": {"folder": "<best-guess-folder>"}}
  ]
}
```

CLI `nt recall` prints the equivalent one-line hint. Purely additive; costs
two dozen tokens; only appears when the compressed layer has, by its own
existing measure, failed. (`include_archived` on `nt_search`, if not already
present, is part of this item — the escape hatch must actually reach the
archived/faded long tail.)

## 7. Documentation-only: the write gate (β)

KDA's β decides whether the current token is written to state at all. nt's
equivalent already half-exists: the idle-nudge threshold and the "what to
skip" bullet in the `/learn` prompts. This spec finishes the thought with an
explicit capture bar in the MCP tool catalog and integration prompts:

> Write a note when something **failed** (lesson), was **decided**
> (decision), or **surprised you** (gotcha). Don't write session narration —
> the logbook already has it. Prefer editing the topic's canonical note
> (`if_exists: "return"`) over creating a sibling.

No code. Listed as a spec item so the prompt change ships and is versioned
with the features it references.

## 8. Testing & evaluation

- **Unit**, per package norms: duration parsing (incl. `none`, garbage, `0d`),
  decay math with injected `now` (the recall determinism tests must keep
  passing — `now` is a parameter, never sampled inside ranking), tier
  arithmetic invariant under fade-rollup (pinned + recent + older == total),
  `if_exists` matching incl. archived/superseded exclusion, `nt decide`
  section creation/prepend/hostile-input refusal, history `--follow` across an
  `nt mv`.
- **Ranking changes are judged on the 50-query corpus**, per the standing
  rule in the memory roadmap (the 22-query corpus was proven too thin — one
  hit is 2pp there, not 4.5pp). Decay ships default-inert (no `half_life`, no
  effect), so the corpus gate applies to the *documented recommendation* to
  adopt it, and to any future class-default: measure HIT@1 with a decayed
  store before recommending half-lives in the README. `NT_LENNORM_B`'s
  history is the cautionary tale — shipped default-off because the measured
  effect didn't earn a default. Decay gets the same honesty bar.
- **Round-trip**: notes carrying the new keys written by an older nt (as
  `Extra` passthrough) load and save byte-identically; a newer nt reading a
  store with no new keys produces byte-identical output to today.
- **Concurrency**: `nt decide`/`nt touch` under a concurrent editor —
  `expect_mtime` refusal path, undo journal restores the before-image.

## 9. Backward compatibility & migration

Nothing migrates. Every mechanism is opt-in per note (`half_life`,
`reviewed`), opt-in per call (`if_exists`, `nt_history.patch`), additive
output (`faded`, `decisions`, `escalate`), or a documentation change (§7).
A store never touched by these features is byte-identical under the new
binary; the new keys degrade to preserved `Extra` lines under an old binary.
No version stamp, no schema bump, no index — there is still nothing that can
drift.

## 10. Implementation phases

| Phase | Scope | Ships when |
|---|---|---|
| 1 | `half_life`/`reviewed` parsing, decay in recall + index + report flags, `nt touch` | Unit green; determinism suite green; corpus baseline recorded |
| 2 | `nt decide`/`nt_decide`, distill/supersede provenance stamps, `nt history`/`nt_history` | Phase 1 merged (decision-log dates feed the review flow) |
| 3 | `if_exists` on `nt_note`/`nt note`, integration prompt updates, escalation hint | Independent of 1–2; smallest diff, could land first |
| 4 | README/SPEC.md sections, `/learn` + skill prompt updates (§7), worked example in docs/claude-integration.md | With whichever phase lands last |

## 11. Open questions

1. `decayFloor` 0.30 and `fadedThreshold` 0.5 are proposals; pick after
   measuring against a real store's age distribution (the eval harness can
   report the decay-factor histogram).
2. Should `nt report`'s faded section cap (like `TierRecentCap`) or list all?
   Leaning cap-with-count, consistent with tier honesty ("nothing hides
   silently").
3. `nt_decide` vs. overloading `nt_note_edit` with a `decision:` arg — one
   more tool in the catalog vs. one more mode on a fat tool. Catalog token
   budget says overload; strict-args clarity says new tool. Undecided.
4. Does `if_exists: "return"` matching want title *aliases* too (Obsidian
   `aliases:` already parse)? Probably yes and cheap, but it widens the
   match surface — decide with a test corpus of real dupe pairs from
   `nt distill` logs.
5. Whether `escalate.try` should ever suggest `nt_history` (it's per-note;
   escalation fires when we *don't* know which note). Current answer: no.

---

*Background: the K3 mechanisms and the documentation analogy are written up
in the research report this spec grew from — "How Kimi K3 Learns More
Efficiently" (Kimi K3 technical report, arXiv:2607.24653; Moonshot's K3
blog/announcement). The report's five principles map to §3 (decay), §4
(delta-writes), §5 (version history), §6 (hybrid access), §7 (write gate).*
