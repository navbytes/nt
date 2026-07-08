# nt ↔ Pi — a memory, rules & knowledge-base system

This bundle turns [`nt`](../../README.md) into the **memory, rules, and
knowledge-base backend** for [Pi](https://github.com/badlogic/pi-mono) (the
minimal terminal coding agent), wired up the way Pi is designed to be extended:
an in-process **extension**, two **prompt templates** (`/recall` in, `/learn`
out), a **skill**, and a thin `AGENTS.md`. The result is a coding agent whose
memory survives across sessions, lives in plain files you can
`grep`/`git diff`/open in Obsidian, and costs the right number of tokens for
each kind of content.

```bash
nt pi install   # complete setup from any installed nt binary (--print to preview)
```

(From a repo checkout, `./install.sh` performs the same steps.)

---

## Pi has no MCP — so the extension *is* the bridge

The one structural difference from the OpenCode integration: **Pi has no
built-in MCP.** Its docs say so plainly — *"No MCP. …build an extension that
adds MCP support."* nt already ships an MCP server (`nt mcp`, newline-delimited
JSON-RPC over stdio), so the `nt-memory` extension **spawns it, lists its
tools, and registers each one as a native Pi tool** (`pi.registerTool`). The
tool names + schemas come straight from `nt mcp`, so `nt_index`, `nt_recall`,
`nt_note`, … show up in Pi exactly as they do under any MCP client, and stay in
sync with whatever nt binary you're running. Disable with `NT_BRIDGE=0` to run
injection-only (the agent then drives nt via the CLI over bash).

---

## The model: three layers, matched to three Pi surfaces

The core design problem is a **token-budget** one. Pi's rules layer — `AGENTS.md`
+ `SYSTEM.md` — is *static text loaded into context*, billed on **every**
request. So the question for each kind of memory is not "can the agent read it?"
but "should it be in context *all the time*?" That splits cleanly into three
layers:

| Layer | What it is | nt home | Pi surface | Token cost |
|-------|-----------|---------|------------|-----------|
| **Rules** | Small, stable directives ("always run gofmt", review process) | `rules/` + tag `rule` | Injected into the system prompt (extension, `before_agent_start`) | Paid every turn → keep tiny |
| **Core memory** | A handful of evolving, always-relevant facts (user prefs, key conventions) | `memory/` + tag `memory-core` | Injected alongside rules | Paid every turn → keep tiny |
| **Knowledge base** | Everything else: findings, decisions, reference, task history | `ref/`, `decisions/`, … | nt tools bridged from `nt mcp` (`nt_index` → `nt_search`/`nt_get`, `nt_links`) | **Zero until queried** |

The discipline that makes this work: **the rules + core-memory core stays
small** (it's always in context), and the **bulk knowledge base stays behind the
tools** (retrieved on demand). Promoting a reference note into a standing rule
is a retag (`nt_tag … +rule`), never a copy.

### Learning from past mistakes — the recall loop

- Record a mistake/footgun/dead-end as a **lesson** — `nt_note` tagged `lesson`
  (CLI `nt note … --lesson`), with the *trigger* in the description
  ("when X, do Y — not Z").
- At the **start of each task**, the agent calls **`nt_recall`** with a
  plain-words description of what it's about to do. Unlike `nt_search` (exact
  substring), `nt_recall` stems and expands dev-concept synonyms, so a
  *paraphrased* task still surfaces the lesson worded differently — with recorded
  **lessons ranked first**.
- And the extension makes the loop fire even when the agent forgets: a **failed
  bash command triggers a lessons-only recall automatically** and appends the
  hits onto the failing tool's result, so the mistake summons its own antidote on
  the very next turn.

Lessons cost tokens only when `nt_recall` returns them — never a standing cost.

---

## The building blocks (what's in this bundle)

### 1. Extension — the bridge + injection + learning loop (`extensions/nt-memory.ts`)

A single in-process TypeScript extension (Pi loads everything under
`~/.pi/agent/extensions/`). It:

- **Bridges nt's tools.** On load it spawns `nt mcp`, does the JSON-RPC
  handshake, and `pi.registerTool`s each advertised tool — the read set
  (`nt_index`, `nt_search`, `nt_recall`, `nt_get`, `nt_status`, `nt_links`) and
  the write/curation set (`nt_add`, `nt_note`, `nt_note_edit`, `nt_update`,
  `nt_tag`, `nt_mv`, `nt_archive`, `nt_relink`, `nt_rm`). The subprocess is torn
  down on `session_shutdown`.
- **Injects rules + core memory** into the system prompt on every agent run via
  the `before_agent_start` hook, recompiled **live from nt** (`nt export`) — edit
  a note in nt and the next run sees it, with no exported file to go stale. The
  injected block is capped at `NT_INJECT_MAX` chars and truncates on **note
  boundaries** (never mid-rule). Because it re-runs each turn, the rules also
  naturally survive context compaction. Set `NT_INJECT=off` to disable.
- **Error-triggered recall** (`NT_ERROR_RECALL=0` to disable) — when a `bash`
  tool call fails, it runs `nt recall --lessons-only` on the command + error tail
  and appends any matching lessons onto the result the model reads next, as an
  `<nt-lessons>` block. One recall per distinct failing command.
- **Idle capture nudge** (`NT_IDLE_NUDGE=0` to disable) — if a session used tools
  but never wrote to nt, a one-time toast (on `agent_end`) suggests running
  `/learn`. User-facing only; never injected into the model context.

Everything is wrapped so a missing or broken nt can never break a session — if
the bridge fails to start, the agent still has the injected rules and falls back
to the `nt` CLI over bash (per the skill/`AGENTS.md`).

### 2. `/recall` prompt template — on-demand memory briefing (`prompts/recall.md`)

The read-side twin of `/learn`. Run **`/recall <topic>`** at the start of a task
and the agent builds a compact **task-priming brief**: recorded lessons opened in
full, related notes as stubs (at most 2 opened), and related open tasks — under a
~1–2K-token budget. Run **`/recall`** bare for a *resume brief* ("where was I?").
Pi expands the template with bash-style args (`$@`).

### 3. `/learn` prompt template — human-gated session harvest (`prompts/learn.md`)

Run `/learn` (optionally `/learn <focus>`) and the agent reviews the session,
extracts candidate learnings in five buckets — **lesson**, **rule**,
**memory-core**, **note**, **task** — dedups them against the store, and presents
a numbered list for approval **before writing anything**. Items headed for the
always-injected layer are flagged with their standing token cost. The approval
gate keeps the injected core small and high-signal.

### 4. Skill — the workflow (`skills/nt/SKILL.md`)

Teaches the agent the recall-first / capture-the-why loop and the folder+tag
conventions, loaded on demand (Pi's `/skill:nt`; auto-loaded when relevant).

### 5. `AGENTS.md` — the thin always-on nudge

A tiny file telling the agent it *has* nt memory, to `nt_index`/`nt_status` at
the start, and to capture as it works. Pi concatenates `AGENTS.md` from
`~/.pi/agent/`, parent dirs, and the cwd. The substance lives in nt, not here.

### 6. `nt export` — the compile primitive

`nt export [--tag T] [--folder F] [--type note|task|all] [--out FILE]
[--no-provenance] [--no-header]` concatenates selected notes into one document —
what the extension uses to build the injected block.

---

## Install & verify

```bash
nt pi install               # from any installed binary (no checkout needed)
nt pi install --print       # preview every step without writing
```
Or, from a repo checkout (e.g. while iterating on the extension):
```bash
cd integrations/pi
./install.sh                # or: NT_BIN=/abs/path/to/nt ./install.sh
```
Both are idempotent; re-running `nt pi install` after an nt upgrade refreshes the
extension/skill/prompts to the versions that binary ships. Then restart Pi (or
run `/reload`). Verify:
```bash
nt export --tag rule --title Rules     # exactly what gets injected as rules
```
In a Pi session, the agent should be able to call `nt_status` / `nt_search` and
you should see a `<nt-memory>` block influencing its behavior.

### Daily use
```bash
nt note "Always prefer table-driven tests" --kind rule --description "…"           # a rule
nt note "User deploys via 'make ship', not CI" --kind memory --description "…"     # core memory
nt note "Auth uses 24h JWTs, 7d refresh" --kind ref --tag auth --description "Token lifetimes"  # KB (on-demand)
```
The agent reads rules+memory every session automatically, and finds the KB note
only when it `nt_search`es for "jwt".

Bracket a working session with the two prompt templates: **`/recall <topic>`** at
the start and **`/learn`** at the end.

---

## Config paths & environment

- Config dir: `~/.pi/agent/` (override with `PI_CODING_AGENT_DIR`). Files land in
  `extensions/nt-memory.ts`, `skills/nt/SKILL.md`, `prompts/{learn,recall}.md`,
  and `AGENTS.md`.
- `nt` must be on Pi's PATH, or set `NT_BIN=/abs/path/to/nt`.

Environment toggles (set on the Pi process):

| Var | Default | Effect |
|-----|---------|--------|
| `NT_INJECT` | `system` | `off` disables rules+memory injection |
| `NT_BRIDGE` | on | `0` skips registering nt's tools (injection-only) |
| `NT_ERROR_RECALL` | on | `0` disables failed-bash → lessons recall |
| `NT_IDLE_NUDGE` | on | `0` disables the idle capture toast |
| `NT_INJECT_MAX` | `8000` | char cap on the injected block |
| `NT_BIN` | `nt` | absolute path to the nt binary |

## Requirements
- `nt` on PATH (or `NT_BIN`).
- Pi with the extension API (`registerTool`, `pi.on(...)`). The bridge needs to
  spawn `nt mcp` (a subprocess); injection needs `before_agent_start`;
  error-recall needs `tool_result`; the idle nudge needs `agent_end`. If the
  bridge can't start, injection and the CLI fallback still work.
