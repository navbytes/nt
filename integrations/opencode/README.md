# nt ↔ OpenCode — a memory, rules & knowledge-base system

This bundle turns [`nt`](../../README.md) into the **memory, rules, and
knowledge-base backend** for [OpenCode](https://opencode.ai), wired the way
OpenCode is extended: an MCP server, a plugin, three commands (`/recall` in,
`/learn` out, `/distill` to consolidate), a skill, and a thin `AGENTS.md`. The result is a coding agent
whose memory survives across sessions, lives in plain files you can
`grep`/`git diff`/open in Obsidian, and costs the right number of tokens per
kind of content.

```bash
nt opencode install   # complete setup from any installed nt binary (--print to preview)
```

(From a repo checkout, `./install.sh` does the same.)

## The model: three layers, matched to three OpenCode surfaces

The core problem is a **token budget** one. OpenCode's rules layer (`AGENTS.md` +
the `instructions` config) is static text billed on **every** request, so the
question per kind of memory is not "can the agent read it?" but "should it be in
context *all the time*?" That splits three ways:

| Layer | What it is | nt home | OpenCode surface | Token cost |
|-------|-----------|---------|------------------|-----------|
| **Rules** | Small, stable directives ("always run gofmt", review process) | `rules/` + tag `rule` | Injected into the system prompt (plugin) | Every turn → keep tiny |
| **Core memory** | A few evolving, always-relevant facts (prefs, key conventions) | `memory/` + tag `memory-core` | Injected alongside rules | Every turn → keep tiny |
| **Knowledge base** | Everything else: findings, decisions, reference, history | `ref/`, `decisions/`, … | nt **MCP tools** (`nt_index` → `nt_search`/`nt_get`, `nt_links`) | **Zero until queried** |

Keep the rules + core-memory core small (always in context); keep the bulk KB
behind the MCP tools (retrieved on demand). Promoting a reference note to a rule
is a retag (`nt_tag … +rule`), never a copy.

### The recall loop — learning from past mistakes

A recorded mistake that's never resurfaced is wasted, so this setup adds a
**lesson** class and a proactive retrieval step, both at zero standing cost:

- Record a mistake/footgun/dead-end as a **lesson** — `nt_note` tagged `lesson`
  (CLI `nt note … --lesson`), with the *trigger* in the description.
- At each **task start** the agent `nt_recall`s a plain-words description of what
  it's about to do. Unlike `nt_search` (exact substring), recall stems + expands
  synonyms, so a paraphrased task still surfaces a differently-worded lesson —
  **lessons ranked first**, with a soft same-project boost when `NT_WORKSTREAM`
  is set.
- The plugin fires the loop even when the agent forgets: a **failed bash command
  auto-triggers a lessons-only recall** into the next request, and lessons
  survive **compaction** (below).

Lessons cost tokens only when recall returns them.

## The building blocks

**1. MCP server (`mcp.nt`)** — `nt mcp` exposes 19 typed tools; OpenCode is a
first-class MCP client, so this *is* the read/write path. Retrieval is
progressive: `nt_index` (cheap stub catalog) → `nt_search` (ranked stubs) →
`nt_get` (one body).
- Read: `nt_index`, `nt_search`, `nt_recall`, `nt_get`, `nt_status`, `nt_links`, `nt_view`
- Write: `nt_add`, `nt_note`, `nt_note_edit`, `nt_update`, `nt_tag`, `nt_mv`, `nt_archive`, `nt_relink`, `nt_rm`

Registered (absolute path, idempotent) by `nt mcp install --client opencode`,
which writes into `~/.config/opencode/opencode.json`:
```json
{ "mcp": { "nt": { "type": "local", "command": ["/abs/nt", "mcp"], "enabled": true,
                   "environment": { "NT_WORKSTREAM": "auto" } } } }
```

**2. Plugin (`plugins/nt-memory.ts`)** — injects the rules + core-memory block
into the system prompt, recompiled live from nt. Fully wrapped so a broken nt
can't break a session. Modes:
- `NT_INJECT=hybrid` *(default)* — writes a **session-start file baseline**
  (the compiled rules+memory block, refreshed on `session.created`, loaded via
  the STABLE `instructions` config — `install` sets `"instructions":
  ["nt-rules.md"]`) AND pushes live updates via
  `experimental.chat.system.transform` whenever the store changes after that
  snapshot, deduped so a working hook never shows the same content twice. This
  exists because `experimental.chat.system.transform` is reported to silently
  discard its mutation on some OpenCode builds ([sst/opencode#17100][17100],
  closed "not planned") — the old `system`-only default had no fallback, so a
  default install on an affected build injected **zero** rules, silently. The
  file baseline can't no-op the same way.
- `NT_INJECT=system` — transform-only, no file baseline (the old default) —
  keep this if you've confirmed your build's transform hook actually reaches
  the model and prefer not to touch `opencode.json`.
- `NT_INJECT=file` — file-only, no live transform push.
- `NT_INJECT=off` — rely on `AGENTS.md` + on-demand MCP.

[17100]: https://github.com/sst/opencode/issues/17100

It also closes the loop automatically (each independently switchable):
- **Compaction survival** (`NT_COMPACT=0`) — on `experimental.session.compacting`
  it pushes open nt tasks + a "re-`nt_recall` before resuming" directive into the
  compaction context.
- **Error-triggered recall** (`NT_ERROR_RECALL=0`) — a non-zero bash exit runs
  `nt recall --lessons-only` on the command + error tail and injects matching
  lessons into the **next** request as `<nt-lessons>`. One recall per distinct
  failing command; injected once, then cleared.
- **Idle nudge** (`NT_IDLE_NUDGE=0`) — a session that used tools but never wrote
  to nt gets one TUI toast suggesting `/learn`.

Optional `NT_MIRROR_TODOS=1` mirrors OpenCode todos → nt tasks on `todo.updated`
(off by default).

**3. `/recall` command (`commands/recall.md`)** — the read-side twin of `/learn`.
`/recall <topic>` builds a compact **task-priming brief** (lessons opened in
full, related notes as stubs with ≤2 opened, related open tasks; ~1–2K-token
budget). Bare `/recall` gives a *resume brief* ("where was I?").

**4. `/learn` command (`commands/learn.md`)** — run `/learn` (optionally with a
focus) and the agent reviews the session, extracts candidates in five buckets
(**lesson**, **rule**, **memory-core**, **note**, **task**), dedups against the
store, and presents a numbered list for approval **before writing**. Items headed
for the always-injected layer are flagged with their standing cost. The approval
gate keeps the injected core small.

**4b. `/distill` command (`commands/distill.md`)** — store consolidation in two
passes. **Pass 1** is the batch counterpart of the write-time near-duplicate
guard: lists every near-duplicate note pair via `nt_distill` (read-only), then
walks each pair to approval before merging (`nt_note_edit` + `nt_archive
superseded_by`) or tagging a deliberate fork `distinct`. **Pass 2** reviews the
always-injected `rules/` + `memory/` block — the part billed on every request —
for subsumption, contradictions, dead triggers, and rules that turned out not to
be always-relevant; the fix for the last is a demotion to a lesson (still found
by `nt_recall`, no longer injected), not a deletion. Both passes land in one
approval list and nothing is written without approval. `/distill rules` runs
pass 2 alone.

**5. Skill (`skills/nt/SKILL.md`)** — the recall-first / capture-the-why loop and
folder+tag conventions, loaded on demand via OpenCode's `skill` tool.

**6. `AGENTS.md`** — a tiny always-on nudge (the substance lives in nt).

**7. `nt export`** — the compile primitive: `nt export [--tag T] [--folder F]
[--type note|task|all] [--out FILE] [--no-provenance] [--no-header]`
concatenates selected notes into one document (what the plugin injects and
file-mode writes to `nt-rules.md`).

## Install & verify

```bash
nt opencode install          # from any installed binary
nt opencode install --print  # preview, change nothing
```
Or from a checkout: `cd integrations/opencode && ./install.sh` (or
`NT_BIN=/abs/nt ./install.sh`). Both are idempotent; re-running after an nt
upgrade refreshes the plugin/skill/commands. Restart OpenCode (or reload MCP),
then verify:
```bash
nt export --tag rule --title Rules            # exactly what gets injected
nt mcp install --client opencode --print      # the MCP entry, without writing
```

```bash
nt note "Always prefer table-driven tests" --kind rule --description "…"       # rule
nt note "User deploys via 'make ship'" --kind memory --description "…"         # core memory
nt note "Auth uses 24h JWTs, 7d refresh" --kind ref --tag auth --description "…"  # KB (on-demand)
```
Bracket a session with **`/recall <topic>`** at the start and **`/learn`** at the
end.

## Choices & trade-offs

- **Global vs per-project.** `install.sh` sets up globally (`~/.config/opencode/`
  over one global store). For project-scoped memory, set `NT_DIR=./.nt` (and `nt
  git-init`) and put `opencode.json`/`.opencode/` in the repo; isolate tasks per
  worktree with `NT_WORKSTREAM` while notes stay shared.
- **Live vs file injection.** `hybrid` (default) gets both: a guaranteed
  session-start file baseline plus live updates when the experimental
  transform hook actually works on your build. `system` is always-fresh but
  depends entirely on that experimental hook with no fallback; `file` is
  stable but only refreshes once per session. Switch with `NT_INJECT`.
- **Token budget is standing cost.** Anything tagged `rule`/`memory-core` is
  billed every turn — audit with `nt export --tag rule` and trim.

## Provider compatibility

Provider-agnostic: everything runs in the OpenCode harness *before* the model
call, so it works whether OpenCode talks to Claude or any model via a LiteLLM
proxy / custom provider (no dependency on hosted models). Install only **merges**
`mcp.nt` + `permission.skill.nt` + `instructions` — your provider/model config is untouched. The
always-in-context layer is plain system-prompt text (no tool-calling needed); the
on-demand `nt_*` tools need the routed model to support tool calling (Claude
does), degrading gracefully otherwise.

## Requirements
- `nt` on PATH (or `NT_BIN`).
- OpenCode with MCP support (all current versions) and a stable `instructions`
  config (all current versions) — the default `hybrid` mode's guaranteed layer
  needs only these. The *live* freshness layer + error-triggered recall use
  `experimental.chat.system.transform`; compaction survival uses
  `experimental.session.compacting` — both experimental and reported to no-op
  on some builds, which is exactly why `hybrid` doesn't depend on them alone.
  The idle nudge and todo mirror use only stable hooks.
- `node` is used only by `install.sh` to merge two config keys; optional.
