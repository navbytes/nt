# nt ↔ OpenCode — a memory, rules & knowledge-base system

This bundle turns [`nt`](../../README.md) into the **memory, rules, and
knowledge-base backend** for [OpenCode](https://opencode.ai), wired up the way
OpenCode is designed to be extended: an MCP server, a plugin, a `/learn`
command, a skill, and a thin `AGENTS.md`. The result is a coding agent whose memory survives across
sessions, lives in plain files you can `grep`/`git diff`/open in Obsidian, and
costs the right number of tokens for each kind of content.

```bash
./install.sh        # global install into ~/.config/opencode
```

---

## The model: three layers, matched to three OpenCode surfaces

The core design problem (from researching OpenCode's extension surfaces) is a
**token-budget** one. OpenCode's rules layer — `AGENTS.md` + the `instructions`
config — is *static text loaded into context*, billed on **every** request. So
the question for each kind of memory is not "can the agent read it?" but "should
it be in context *all the time*?" That splits cleanly into three layers:

| Layer | What it is | nt home | OpenCode surface | Token cost |
|-------|-----------|---------|------------------|-----------|
| **Rules** | Small, stable directives ("always run gofmt", review process) | `rules/` + tag `rule` | Injected into the system prompt (plugin) | Paid every turn → keep tiny |
| **Core memory** | A handful of evolving, always-relevant facts (user prefs, key conventions) | `memory/` + tag `memory-core` | Injected alongside rules | Paid every turn → keep tiny |
| **Knowledge base** | Everything else: findings, decisions, reference, task history | `ref/`, `decisions/`, … | nt **MCP tools** (`nt_index` → `nt_search`/`nt_get`, `nt_links`) | **Zero until queried** |

The discipline that makes this work: **the rules + core-memory core stays
small** (it's always in context), and the **bulk knowledge base stays behind the
MCP tools** (retrieved on demand). Promoting a reference note into a standing
rule is a retag (`nt_tag … +rule`), never a copy.

### Learning from past mistakes — the recall loop

The knowledge base is only useful if the agent actually *re-reads* the right note
at the right moment. A recorded mistake that's never resurfaced is wasted. So this
setup adds a **lesson** class and a proactive retrieval step, both at zero standing
token cost:

- Record a mistake/footgun/dead-end as a **lesson** — `nt_note` tagged `lesson`
  (CLI `nt note … --lesson`), with the *trigger* in the description
  ("when X, do Y — not Z").
- At the **start of each task**, the agent calls **`nt_recall`** with a plain-words
  description of what it's about to do. Unlike `nt_search` (exact substring),
  `nt_recall` stems and expands dev-concept synonyms, so a *paraphrased* task
  ("adding parallel request handling") still surfaces the lesson worded differently
  ("goroutine deadlock") — with recorded **lessons ranked first**.
- And the plugin makes the loop fire even when the agent forgets: a **failed bash
  command triggers a lessons-only recall automatically** and pipes the hits into
  the next request, and lessons survive **context compaction** (see the plugin
  section below).

This closes the learn-from-sessions loop: mistakes are captured as a distinct,
recall-able class and re-surfaced before they recur — without bloating the
always-injected block (lessons cost tokens only when `nt_recall` returns them).

This mirrors the emerging best practice for OpenCode memory (e.g. Letta-style
"memory blocks": small labelled markdown blocks injected into context, plus
dedicated tools for the agent to maintain them) — except the blocks, tools,
search, links, and history are all just `nt`, which you already use from the CLI,
TUI, web UI, and Obsidian.

---

## The building blocks (what's in this bundle)

### 1. MCP server — the read/write engine (`mcp.nt`)
`nt mcp` exposes 18 typed tools. OpenCode is a first-class MCP client, so this
*is* the knowledge-base + memory read/write path — no custom OpenCode tool
needed. Retrieval follows progressive disclosure: `nt_index` (cheap catalog of
stubs) → `nt_search` (ranked stubs) → `nt_get` (one note's body). No bulk dump.

- **Read:** `nt_index`, `nt_search`, `nt_recall`, `nt_get`, `nt_ready`, `nt_status`, `nt_links`, `nt_view`, `nt_log`
- **Write:** `nt_add`, `nt_note`, `nt_done`, `nt_update`, `nt_tag`, `nt_mv`, `nt_archive`, `nt_supersede`, `nt_relink`

Registered (absolute path, idempotent) by:
```bash
nt mcp install --client opencode
```
which writes OpenCode's schema into `~/.config/opencode/opencode.json`:
```json
{ "mcp": { "nt": { "type": "local", "command": ["/abs/nt", "mcp"], "enabled": true,
                   "environment": { "NT_WORKSTREAM": "auto" } } } }
```

### 2. Plugin — injection + the automated learning loop (`plugins/nt-memory.ts`)
Injects the **rules + core-memory** block into the system prompt, recompiled
**live from nt every session** via the `experimental.chat.system.transform`
hook. Edit a note in nt → the next session sees it. No exported file to go stale.
Compiles with `nt export` and is fully wrapped so a missing/broken nt can never
break a session.

Three modes (set env on the OpenCode process):
- `NT_INJECT=system` *(default)* — live injection via the system-prompt transform.
- `NT_INJECT=file` — instead refresh `~/.config/opencode/nt-rules.md` on
  `session.created` and load it through the `instructions` config (use this if
  your OpenCode build lacks the experimental hook). Add to `opencode.json`:
  `"instructions": ["nt-rules.md"]`.
- `NT_INJECT=off` — inject nothing; rely on `AGENTS.md` + on-demand MCP.

The plugin also closes the learning loop automatically (each on by default,
independently switchable):

- **Compaction survival** (`NT_COMPACT=0` to disable) — on
  `experimental.session.compacting` it pushes the open nt tasks and a
  "re-`nt_recall` before resuming" directive into the compaction context, so
  summarization doesn't drop the in-flight work or the memory workflow.
- **Error-triggered recall** (`NT_ERROR_RECALL=0` to disable) — when a bash tool
  call exits non-zero, the plugin runs `nt recall --lessons-only` on the command
  + error tail and injects any matching lessons into the **next** model request
  as an `<nt-lessons>` block. Recorded mistakes stop relying on the agent
  remembering to ask — the failure summons its own antidote. One recall per
  distinct failing command; the block is injected once, then cleared (a single
  prompt-cache miss per failure, no standing token cost).
- **Idle capture nudge** (`NT_IDLE_NUDGE=0` to disable) — if a session used
  tools but never wrote to nt, a one-time TUI toast suggests running `/learn`.
  User-facing only; never injected into the model context.

Optional: `NT_MIRROR_TODOS=1` mirrors OpenCode's todo list into nt tasks on
`todo.updated` (the OpenCode analog of Claude Code's `nt hook`). Off by default —
the agent already captures tasks via `nt_add`.

### 3. `/learn` command — human-gated session harvest (`commands/learn.md`)
A user-invoked slash command: run `/learn` (optionally `/learn <focus>`) at any
point and the agent reviews the session, extracts candidate learnings in five
buckets — **lesson**, **rule**, **memory-core**, **note**, **task** — dedups
them against the store (`nt_recall`/`nt_search`), and presents a numbered list
for approval **before writing anything**. Items headed for the always-injected
layer (`rule`/`memory-core`) are flagged with their standing token cost, and the
procedure is deliberately stingy there and generous with lessons/notes. The
approval gate is what keeps the injected core small and high-signal — the
opposite failure mode of silent auto-capture. The idle nudge (below) points at
this command.

### 4. Skill — the workflow (`skills/nt/SKILL.md`)
Teaches the agent the recall-first / capture-the-why loop and the folder+tag
conventions, loaded on demand via OpenCode's `skill` tool (its description sits
in context; the body loads only when relevant — progressive disclosure).

### 5. `AGENTS.md` — the thin always-on nudge
A tiny file telling the agent it *has* nt memory, to `nt_index`/`nt_ready` at
the start, capture as it works, and how to lazy-load `@`-references (OpenCode does
not auto-expand them). The substance lives in nt, not here.

### 6. `nt export` — the compile primitive
`nt export [--tag T] [--folder F] [--type note|task|all] [--format md|json]
[--out FILE] [--no-provenance]` concatenates selected notes (and optionally open
tasks) into one document — what the plugin uses to build the injected block and
what file-mode writes to `nt-rules.md`. Each note carries a
`<!-- nt:<id> <path> -->` provenance line (suppressed with `--no-provenance`) so
the compiled output traces back to its source note by **stable nt id**.

---

## Install & verify

```bash
cd integrations/opencode
./install.sh                      # or: NT_BIN=/abs/path/to/nt ./install.sh
```
Then restart OpenCode (or reload MCP). Verify:
```bash
nt export --tag rule --title Rules     # exactly what gets injected as rules
nt mcp install --client opencode --print   # the MCP entry, without writing
```
In an OpenCode session, the agent should be able to call `nt_ready` / `nt_search`
and you should see a `<nt-memory>` block influencing its behavior.

### Daily use
```bash
nt note "Always prefer table-driven tests" --folder rules  --tag rule          # a rule
nt note "User deploys via 'make ship', not CI" --folder memory --tag memory-core  # core memory
nt note "Auth uses 24h JWTs, 7d refresh" --folder ref --tag auth               # KB (on-demand)
```
The agent reads rules+memory every session automatically, and finds the KB note
only when it `nt_search`es for "jwt".

At the end of a working session, run **`/learn`** in OpenCode: the agent
proposes the session's learnings (deduped against the store) and saves only
what you approve.

---

## Choices & trade-offs

- **Global vs per-project.** `install.sh` does a global setup
  (`~/.config/opencode/`) over a single global nt store — personal memory across
  all projects. For project-scoped memory, set `NT_DIR=./.nt` (and
  `nt git-init`) and place `opencode.json` / `.opencode/` in the repo; tasks can
  be isolated per worktree with `NT_WORKSTREAM` while notes stay shared.
- **Live injection vs static file.** Default (`system`) is always-fresh but uses
  an *experimental* OpenCode hook; `file` mode is fully documented/stable but
  refreshes once per session. Switch with `NT_INJECT`.
- **Agent-driven vs passive capture.** Capture quality is highest when the agent
  deliberately writes notes (guided by the skill + `AGENTS.md`) rather than
  auto-summarizing. The todo mirror (`NT_MIRROR_TODOS`) is the one passive option,
  off by default.
- **Token budget is a standing cost.** Anything tagged `rule`/`memory-core` is
  billed every turn. Audit it occasionally with `nt export --tag rule` and trim.

## Provider compatibility (LiteLLM / BYO models)

This integration is **provider-agnostic**. Everything here runs in the OpenCode
harness *before* the model call, so it works identically whether OpenCode talks
to Claude or any other model through a LiteLLM proxy or a custom provider — it
does **not** depend on OpenCode's hosted ("Zen") models.

- `nt mcp install` and `install.sh` only **merge** `mcp.nt` and
  `permission.skill.nt`; your `provider` / `model` / endpoint config is left
  untouched.
- **Always-in-context layer** (rules + core memory via the plugin, plus
  `AGENTS.md`) is injected as plain system-prompt text — **no tool-calling
  required**, so it works on every model/route.
- **On-demand KB layer** (the `nt_*` MCP tools) requires the routed model to
  support **tool/function calling** through LiteLLM. Claude does. On a model with
  weak tool support that layer degrades gracefully — the injected rules/memory
  still apply.

## Requirements
- `nt` on PATH (or `NT_BIN`).
- OpenCode with MCP support (all current versions). Live injection needs the
  `experimental.chat.system.transform` hook (error-triggered recall rides the
  same hook); compaction survival needs `experimental.session.compacting`. Both
  are experimental OpenCode APIs — if your build lacks them the plugin degrades
  gracefully (use `NT_INJECT=file` for the rules path). The idle nudge and todo
  mirror use only stable event hooks.
- `node` is used only by `install.sh` to merge one config key; optional.
