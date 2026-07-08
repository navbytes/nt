# nt ↔ Pi — a memory, rules & knowledge-base system

This bundle makes [`nt`](../../README.md) the **memory, rules, and
knowledge-base backend** for [Pi](https://github.com/badlogic/pi-mono) (the
minimal terminal coding agent), wired the way Pi is extended: an in-process
**extension**, two **prompt templates** (`/recall` in, `/learn` out), a
**skill**, and a thin `AGENTS.md`. The agent's memory then survives across
sessions, lives in plain files you can `grep`/`git diff`/open in Obsidian, and
costs the right number of tokens for each kind of content.

```bash
nt pi install   # complete setup from any installed nt binary (--print to preview)
```

(From a repo checkout, `./install.sh` does the same.)

## Pi has no MCP — so the extension *is* the bridge

The one structural difference from the OpenCode integration: **Pi has no
built-in MCP** — its docs say *"No MCP. …build an extension that adds MCP
support."* nt already ships an MCP server (`nt mcp`, newline-delimited JSON-RPC
over stdio), so the `nt-memory` extension **spawns it, lists its tools, and
registers each as a native Pi tool** (`pi.registerTool`). Names + schemas come
straight from `nt mcp`, so `nt_index`, `nt_recall`, `nt_note`, … appear exactly
as under any MCP client and stay in sync with the binary. `NT_BRIDGE=0` runs
injection-only (the agent then drives nt via the CLI over bash).

## Three layers, matched to three Pi surfaces

The core problem is a **token budget** one: Pi's rules layer (`AGENTS.md` +
`SYSTEM.md`) is static text billed on **every** request, so the question per
kind of memory is "should it be in context *all the time*?" That splits three
ways:

| Layer | What it is | nt home | Pi surface | Token cost |
|-------|-----------|---------|------------|-----------|
| **Rules** | Small, stable directives ("always run gofmt") | `rules/` + tag `rule` | Injected into the system prompt (`before_agent_start`) | Every turn → keep tiny |
| **Core memory** | A few evolving, always-relevant facts | `memory/` + tag `memory-core` | Injected alongside rules | Every turn → keep tiny |
| **Knowledge base** | Everything else: findings, decisions, reference | `ref/`, `decisions/`, … | nt tools bridged from `nt mcp` | **Zero until queried** |

Keep the rules + core-memory core small; keep the bulk KB behind the tools.
Promoting a reference note to a rule is a retag (`nt_tag … +rule`), never a copy.

**Recall loop.** Record a mistake as a **lesson** (`nt_note` tagged `lesson`,
trigger in the description). At each task start the agent `nt_recall`s a
plain-words description — paraphrase-aware, lessons ranked first. And a **failed
bash command auto-triggers a lessons-only recall** whose hits are appended onto
the result, so the mistake summons its own antidote next turn. Lessons cost
tokens only when recalled.

## What's in the bundle

- **`extensions/nt-memory.ts`** — the whole system, defensively wrapped so a
  broken nt never breaks a session:
  - **Tool bridge** — spawns `nt mcp`, handshakes, `registerTool`s each nt tool
    (read: `nt_index`/`nt_search`/`nt_recall`/`nt_get`/`nt_status`/`nt_links`;
    write: `nt_add`/`nt_note`/`nt_note_edit`/`nt_update`/`nt_tag`/`nt_mv`/
    `nt_archive`/`nt_relink`/`nt_rm`); torn down on `session_shutdown`. Since
    `registerTool` only runs once at load but the subprocess can die mid
    session (a crash, or `session_shutdown` firing on something short of a
    real end — e.g. `/new` or `/fork`), the bridge **self-heals**: the next
    bridged call lazily respawns it, with concurrent calls sharing one
    in-flight respawn and a cooldown bounding retries against a persistently
    broken `nt`.
  - **Rules + core-memory injection** every run (`before_agent_start`),
    recompiled live from `nt export`, capped at `NT_INJECT_MAX` and truncated on
    note boundaries (never mid-rule). Re-running each turn means rules also
    survive compaction. `NT_INJECT=off` disables.
  - **Error-triggered recall** (`tool_result`, `NT_ERROR_RECALL=0` to disable).
  - **Idle nudge** (`agent_end`, `NT_IDLE_NUDGE=0` to disable) — one toast per
    session suggesting `/learn` when tools were used but nothing was saved.
  - If the bridge can't start, the injected rules still apply and the agent
    falls back to the `nt` CLI.
- **`skills/nt/SKILL.md`** — the recall-first / capture-the-why workflow
  (`/skill:nt`).
- **`prompts/{learn,recall}.md`** — human-gated session harvest + on-demand
  briefing (Pi's bash-style `$@` args).
- **`AGENTS.md`** — a thin nudge (Pi concatenates it from `~/.pi/agent/`, parent
  dirs, and cwd). **`README.md`**, **`install.sh`**, **`embed.go`**.

## Install & verify

```bash
nt pi install          # from any installed binary
nt pi install --print  # preview, change nothing
```
Or from a checkout: `cd integrations/pi && ./install.sh` (or `NT_BIN=/abs/nt
./install.sh`). Both are idempotent; re-running after an nt upgrade refreshes the
files. Then restart Pi (or `/reload`) and verify with `nt export --tag rule
--title Rules` — exactly what gets injected. In a session the agent should call
`nt_status`/`nt_search` and act on a `<nt-memory>` block.

```bash
nt note "Always prefer table-driven tests" --kind rule --description "…"       # rule
nt note "User deploys via 'make ship'" --kind memory --description "…"         # core memory
nt note "Auth uses 24h JWTs, 7d refresh" --kind ref --tag auth --description "…"  # KB (on-demand)
```
Bracket a session with **`/recall <topic>`** at the start and **`/learn`** at the
end.

## Config & environment

Config dir `~/.pi/agent/` (override with `PI_CODING_AGENT_DIR`); files land in
`extensions/nt-memory.ts`, `skills/nt/SKILL.md`, `prompts/{learn,recall}.md`,
`AGENTS.md`. `nt` must be on Pi's PATH, or set `NT_BIN`.

| Var | Default | Effect |
|-----|---------|--------|
| `NT_INJECT` | `system` | `off` disables rules+memory injection |
| `NT_BRIDGE` | on | `0` skips registering nt's tools (injection-only) |
| `NT_ERROR_RECALL` | on | `0` disables failed-bash → lessons recall |
| `NT_IDLE_NUDGE` | on | `0` disables the idle toast |
| `NT_INJECT_MAX` | `8000` | char cap on the injected block |
| `NT_BIN` | `nt` | absolute path to the nt binary |

## Requirements
`nt` on PATH (or `NT_BIN`). Pi with the extension API (`registerTool`, `pi.on`).
The bridge spawns `nt mcp`; if it can't start, injection and the CLI fallback
still work.
