# nt + OpenCode

This document answers the OpenCode integration brief: use `nt` as the **rules,
memory, and knowledge-base** backend for [OpenCode](https://opencode.ai) (the
current Anomaly/SST version with a JS server + Go TUI, repo
`github.com/anomalyco/opencode`).

It is grounded in an audit of this repository's source and runtime, not the
binary's name. **Headline finding:** `nt` is already a non-interactive,
JSON-emitting CLI *and* ships a stdio **MCP server** (`nt mcp`) with 18 typed
read **and write** tools — and OpenCode natively consumes MCP servers. That
collapses most of what the brief assumed "must be built." The agent-driven
read/write loop (knowledge-base retrieval **and** write-back memory) needs **no
custom OpenCode tool or plugin** — only a one-line MCP registration, now wired by
`nt mcp install --client opencode`. The only genuinely new piece is a small
**export step** for the always-in-context *rules* file.

---

## 1. Capability report (answers to the brief's §3.1)

Audited against `internal/` and a built binary (`v0.10.0`-era `main`).

| # | Question | Finding |
|---|----------|---------|
| 1 | **Purpose & data model** | Task + note manager built explicitly as "durable memory for AI sessions." Tasks are todo.txt-style lines in `tasks.txt`; notes are markdown files with YAML frontmatter under `notes/` **with subfolders** (`ref/`, `decisions/`, `notes/__tasks__/` for task bodies, `notes/journal/` for dailies). Plain files — no DB. |
| 2 | **Interface** | A Go CLI (`nt <verb>`), **plus** a stdio **MCP server** (`nt mcp`), **plus** a localhost web HTTP API (`nt web`), **plus** a Bubble Tea TUI and a Wails desktop app. Four programmatic surfaces; three are non-interactive. |
| 3 | **Output format** | Every read/write verb takes `--json` and prints structured JSON to stdout; non-`--json` output is human text. Fully pipeable and scriptable. |
| 4 | **Read / query** | Index-first progressive disclosure: `nt index [--tag --folder --json]` (a catalog of note **stubs** — id, title, one-line description, tags — plus active tasks, **no bodies**); `nt search "<text>" [--tag] [--limit N] [--json]` (ranked stubs, `--full` for bodies); `nt show <handle>` / `nt_get` (one note's body, optional `section`); plus `nt ready`, `nt status`, `nt log`, `nt links <handle> [--orphans]`, `nt tags`, `nt view <name>`. |
| 5 | **Write / append** | **Fully non-interactive.** `nt add`, `nt note` (with `--body`, `--folder`, `--field k=v`), `nt done`, `nt update`, `nt tag`, `nt mv`, `nt rm`, `nt archive` — all flag/arg-driven, all with `--json`. No `$EDITOR`/GUI required (`nt edit`/`nt journal` are the *opt-in* interactive verbs). |
| 6 | **Addressability** | Every entry has a stable **ULID** `id` (e.g. `01KW8N…`). Notes also addressable by slug/title. Any verb accepts the same **handle** (id, slug, or title) it printed. The MCP layer **refuses** positional `task:N` — agents must use stable ids. |
| 7 | **Storage location & portability** | Global store at `$NT_DIR` (default `~/.local/share/nt`); `nt path` prints it. Fully relocatable via the env var — can point into a repo. `nt git-init` sets up union-merge + `.gitignore` for committing the store. |
| 8 | **Concurrency & safety** | `tasks.txt.lock` flock + atomic temp-then-rename writes (`internal/store/atomic.go`, `internal/lock/`) + an undo journal. Safe to read while OpenCode (or a human TUI/web session) writes. **Workstreams** (`NT_WORKSTREAM`) isolate parallel agents' *tasks* while keeping *notes* shared. |
| 9 | **Existing markdown compatibility** | Notes are already markdown-with-frontmatter on disk — directly consumable by OpenCode's `instructions` globs and as `SKILL.md` bodies. No transform needed to *read* them as files. |
| 10 | **Scale & latency** | File-backed, in-process; queries are local fs scans returning in milliseconds for typical personal corpora. A synchronous MCP/CLI call returns well within OpenCode's tool budget. Very large corpora → lean on tags/`--type` to bound results. |
| 11 | **Language/runtime** | Single static **Go** binary, zero runtime deps. Trivial to shell out to or launch as a stdio MCP subprocess on the same machine as OpenCode's Bun server. |

**Read/write verdict:** ✅ both. `nt` is a non-interactive, scriptable,
concurrency-safe, stable-id store that already speaks MCP. The brief's worst-case
branches ("interactive/GUI-only writes", "library not a CLI", "must build the
write path") **do not apply**.

---

## 2. The reframe — what the brief didn't know

The brief was written without access to `nt` and reasoned that OpenCode's
read-only `AGENTS.md`/`instructions` layer means **"letting the agent capture new
memory must be built"** (a custom `nt_save` tool and/or a plugin). Two facts
change the design:

1. **`nt` already exposes write tools over MCP** — `nt_add`, `nt_note`,
   `nt_done`, `nt_update`, `nt_tag`, `nt_mv`, `nt_archive`, `nt_supersede`, `nt_relink` — alongside the read
   tools `nt_index`, `nt_search`, `nt_recall`, `nt_get`, `nt_ready`, `nt_status`, `nt_view`,
   `nt_log`, `nt_links`. Verified end-to-end over stdio JSON-RPC (`tools/list` →
   18 tools; `tools/call nt_index`/`nt_search`/`nt_get` → results).
2. **OpenCode is a first-class MCP client.** Its config has a top-level `mcp` key
   for `"type": "local"` (stdio) servers, and the agent calls those tools the
   same way it calls built-ins.

So the entire **memory + knowledge-base read/write loop is satisfied by
registering one MCP server** — no OpenCode-side custom tool or plugin code. This
PR adds that registration path:

```bash
nt mcp install --client opencode          # writes ~/.config/opencode/opencode.json
nt mcp install --client opencode --print  # show the snippet, change nothing
```

It writes nt under OpenCode's schema (note: `mcp`, not Claude's `mcpServers`;
`command` is an argv **array**), preserving every other key and stamping
`$schema` on a fresh file:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "nt": { "type": "local", "command": ["/abs/path/to/nt", "mcp"], "enabled": true }
  }
}
```

The remaining custom work is **only** the always-in-context *rules* file, because
that lives in OpenCode's static `instructions`/`AGENTS.md` layer, which has no
write-back API. That needs an `nt export` step (see §4, Phase 1) — small, and the
only net-new `nt` feature recommended.

---

## 3. Recommended architecture

Map each concept to the OpenCode surface that fits its access pattern and token
cost.

| Concept | Surface | Mechanism | Token cost |
|---------|---------|-----------|-----------|
| **Rules** (small, stable, always true) | `instructions` glob → an `nt`-generated markdown file (or `AGENTS.md`) | `nt export --tag rule > .opencode/nt-rules.md`; `"instructions": [".opencode/nt-rules.md"]` | Paid every request — keep it small |
| **Knowledge base** (large, queried occasionally) | **MCP tools** `nt_index` → `nt_search` / `nt_get` / `nt_links` / `nt_status` | Already registered via `nt mcp install --client opencode` | **Zero until called** (lazy, index-first) |
| **Memory write-back** (capture as the agent works) | **MCP tools** `nt_add` / `nt_note` / `nt_done` / `nt_update` | Same registration; agent calls them explicitly | Only when writing |
| **Curated KB highlights** (optional) | **Agent Skills** | Symlink/export curated notes to `.opencode/skills/<name>/SKILL.md` | Only the skill list is always-loaded; bodies load on demand |

**Token-budget plan.** Always-in-context = the rules file only (and OpenCode's
skill *list*, if used). Everything else — the whole note corpus, completed-task
history, link graph — stays behind MCP tools and is fetched on demand. This is
the brief's §2.3 rule of thumb, and it's exactly what `nt`'s plain-file,
index-first retrieval design is built for ("read only what's relevant").

**Why MCP over a custom OpenCode tool or the web HTTP API:**
- The MCP tools are typed, default `source` to `claude`, enforce stable ids, and
  run through the same locked/journaled engine as the CLI — no CLI-string
  assembly, no reimplementation.
- The `nt web` HTTP API exists but is localhost-only, CSRF-gated for writes, and
  shaped for the browser UI — not the right integration seam for an agent. Prefer
  MCP. (If a *shared multi-client* service is ever needed, that's the place to
  add an agent-facing HTTP mode — see §4 Phase 4.)

---

## 4. Phased implementation plan

### Phase 0 — Audit ✅ (this document)
Capability report complete; read+write verdict positive.

### Phase 1 — Rules + static KB ✅ (shipped here)
- **`nt export`** (`--tag`/`--folder`/`--type`/`--format md|json`/`--out`/
  `--no-provenance`) concatenates selected notes into one document for the
  always-in-context layer. **Done.**
- **Live injection.** The `nt-memory` plugin compiles `nt export --tag rule`
  (+ `memory-core`) and injects it into the system prompt every session via
  `experimental.chat.system.transform` — no stale file. File-mode fallback writes
  `nt-rules.md` and loads it via `instructions`. See
  [`integrations/opencode/`](../integrations/opencode/).
- **Curated KB as Skills.** The bundled `skills/nt/SKILL.md` teaches the workflow;
  any high-value note can be surfaced the same way (progressive disclosure).

### Phase 2 — On-demand KB retrieval ✅ (shipped here)
- `nt mcp install --client opencode` registers the stdio server; the agent gets
  `nt_index`, `nt_search`, `nt_get`, `nt_status`, `nt_links`, `nt_view`
  immediately (index-first progressive disclosure). No custom tool needed.

### Phase 3 — Write-back memory ✅ engine + automation (shipped)
- The write tools (`nt_add`, `nt_note`, `nt_done`, `nt_update`, `nt_tag`,
  `nt_mv`, `nt_archive`, `nt_supersede`, `nt_relink`) are live the moment the MCP server is registered — the
  agent captures memory by calling them. **No `nt_save` to build.**
- **Automation (shipped in the `nt-memory` plugin, each on by default):**
  - *Compaction survival* — `experimental.session.compacting` pushes open nt
    tasks + a re-`nt_recall` directive into the compaction context, so
    summarization doesn't drop in-flight work or the memory workflow
    (`NT_COMPACT=0` disables).
  - *Error-triggered recall* — a failed bash call runs
    `nt recall --lessons-only` on the command + error and injects matching
    lessons into the next request; recorded mistakes resurface exactly when
    they're about to repeat (`NT_ERROR_RECALL=0` disables).
  - *Idle capture nudge* — a session that used tools but wrote nothing to nt
    gets a one-time TUI toast suggesting a note/lesson (`NT_IDLE_NUDGE=0`
    disables).
  - In file mode, `session.created` still re-runs `nt export` so Phase 1's
    rules file is fresh each session; `NT_MIRROR_TODOS=1` optionally mirrors
    OpenCode todos into nt (the analog of Claude Code's PostToolUse `nt hook`).

### Phase 4 — Service + governance (optional)
- **Multi-client service.** If several OpenCode clients should share one store
  over the network, add an agent-facing `nt serve` HTTP mode (auth + JSON,
  reusing the engine) rather than exposing the browser API. Then a `"type":
  "remote"` MCP entry or a thin custom tool can target it.
- **Governance.** Use OpenCode `permission.skill` / `permission` to gate which
  nt tools/skills are allowed per agent; keep store-in-repo vs store-in-home as a
  policy choice (see §5).

---

## 5. Open questions & risks

- **Always-loaded token cost.** The rules file is billed every request. Keep
  `nt export --tag rule` output to genuinely-stable, must-always-apply rules;
  push everything else behind `nt_search`/Skills.
- **Synchronous tool latency.** `nt` queries are local-fs and fast. `nt_search`
  now returns bounded stubs (default `limit` 8, `truncated` flag) rather than
  bodies, so payloads stay small; the agent `nt_get`s only the note it needs.
- **Write concurrency.** flock + atomic writes make concurrent agent/human writes
  safe, but a human editing a note in Obsidian *and* an agent `nt mv`-ing it
  could still surprise the human. The undo journal mitigates; document it.
- **Memory across repos/worktrees.** The store is global by default, so memory is
  shared across every OpenCode project unless `NT_DIR` is set per-repo. For
  parallel agents on one store, set `NT_WORKSTREAM` (literal id, or `auto` from
  the git branch) in the MCP entry's `environment` to isolate tasks while sharing
  notes:
  ```json
  { "mcp": { "nt": { "type": "local", "command": ["/abs/nt", "mcp"],
    "environment": { "NT_WORKSTREAM": "auto" } } } }
  ```
- **In-repo vs in-home store.** In-repo (`NT_DIR=./.nt` + `nt git-init`) →
  shared, committed, team-visible memory. In-home (default) → personal, private.
  A team choice, not a technical one; both work.
- **`nt export` is the lone dependency.** Phases 2–3 work today; Phase 1's rules
  path is blocked only on adding `nt export`. Until then, rules can be maintained
  by hand in `AGENTS.md` and KB/memory go fully through MCP.

---

## What ships for OpenCode

- `nt mcp install --client opencode` — registers nt's MCP server into
  `~/.config/opencode/opencode.json` (`mcp` key, `local` type, argv `command`),
  idempotent, `$schema`-stamped, `--print`-able. Delivers Phases 2–3's engine.
- `nt export` — compiles tagged/foldered notes (and open tasks) into one md/json
  document for the always-in-context layer. Delivers Phase 1's compile step.
- **[`integrations/opencode/`](../integrations/opencode/)** — a ready-to-use
  bundle: the `nt-memory` plugin (live rules/memory injection + the automated
  learning loop), the `/learn` command (human-gated session harvest), the `nt` skill,
  a thin `AGENTS.md`, an example `opencode.json`, and an idempotent `install.sh`.
  Its README is the full architecture + best-practices write-up and the
  folder/tag conventions (`rules/`+`rule`, `memory/`+`memory-core`, everything
  else = on-demand KB).
