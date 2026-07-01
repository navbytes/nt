# nt + Claude Code

`nt` is built to be the **durable memory layer for AI coding sessions** — the
place where the action items and notes an agent produces survive after the
session ends, in plain text the next session can read back. There are two
integration points: an automatic **hook** and an explicit **`/nt` skill**.

---

## 1. Automatic capture — the PostToolUse hook

Claude Code maintains a todo list via its `TodoWrite` tool. The `nt hook` command
mirrors that list into your nt store, idempotently, tagged `src:claude`.

### Setup

Add a PostToolUse hook to your Claude Code settings (`~/.claude/settings.json`
for all projects, or a project's `.claude/settings.json`):

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "TodoWrite",
        "hooks": [
          { "type": "command", "command": "nt hook" }
        ]
      }
    ]
  }
}
```

That's it. From then on, whenever Claude updates its todo list:

- new todos are added to nt as tasks (`src:claude`),
- status changes are mirrored (`in_progress` → doing, `completed` → done),
- nothing is duplicated — a per-session map (`$NT_DIR/.claude-sync.json`) tracks
  which todo maps to which nt task.

### How it behaves

| TodoWrite status | nt task |
|------------------|---------|
| `pending`        | open    |
| `in_progress`    | `s:doing` |
| `completed`      | done (`x`) |

`nt hook` reads the hook's JSON event from stdin, is silent, and **always exits
0** — it can never break or slow your session. If `nt` isn't installed or the
store can't be opened, it simply does nothing.

> Tip: while the hook runs, keep the `nt` TUI open in a side pane (`nt`). Tasks
> appear within ~80ms (fsnotify) as Claude works — no window switching.

---

## 2. Explicit capture — the `/nt` skill

The bundled skill ([.claude/skills/nt/SKILL.md](../.claude/skills/nt/SKILL.md))
teaches Claude to use `nt` directly. With it installed, you can say things like:

- "what should I work on?" → Claude runs `nt ready` (open, unblocked, by urgency)
- "save that as a task in nt"
- "note this finding for later"
- "what did we capture last session?" → Claude runs `nt index --source claude`
- or just type `/nt`

Claude will run the right `nt` commands (`ready`, `add`, `note`, `index`,
`show`, `done`, `links`, `search`), always passing `--source claude`.

**Start a session with `nt ready`.** It returns only actionable work — open
tasks that aren't done and aren't waiting on a dependency — newest-urgency
first. That's the agent's "pick up here" feed; `nt index` is the broader
"everything we captured" read — a compact catalog of note stubs (title,
description, tags) plus the active tasks, from which Claude fetches a specific
body with `nt show <handle>` on demand.

Install it by keeping `.claude/skills/nt/` in your project, or copy it to
`~/.claude/skills/nt/` to make it available everywhere.

---

## 3. Typed tools — the MCP server

For clients that speak the **Model Context Protocol** (Claude Code, Cursor, …),
`nt mcp` runs a stdio MCP server so the agent calls **typed tools** instead of
constructing CLI strings — more reliable, and discoverable via `tools/list`.

Register it in one command. It uses the **absolute** binary path (GUI clients
often launch without `~/.local/bin` on `PATH`, so a bare `nt` wouldn't resolve)
and is idempotent:

```bash
nt mcp install                          # Claude Code (user scope)
nt mcp install --client claude-desktop  # Claude Desktop
nt mcp install --print                  # show what it would do, change nothing
```

- **Claude Code** does *not* read MCP servers from `settings.json`. `nt mcp
  install` shells out to `claude mcp add-json nt … --scope user` (the supported
  path) when the `claude` CLI is on `PATH`, and otherwise merges the correct file,
  `~/.claude.json`, directly. Equivalent by hand:

  ```bash
  claude mcp add-json nt '{"type":"stdio","command":"/abs/path/to/nt","args":["mcp"]}' --scope user
  ```

- **Claude Desktop** has no CLI, so it edits `claude_desktop_config.json`
  (macOS: `~/Library/Application Support/Claude/`). By hand, add under a
  top-level `mcpServers`:

  ```json
  { "mcpServers": { "nt": { "type": "stdio", "command": "/abs/path/to/nt", "args": ["mcp"] } } }
  ```

For any other client (Cursor, a project `.mcp.json`, …), `nt mcp install --print`
emits the snippet to paste.

Tools exposed (**18**) — **capture:** `nt_add`, `nt_note` (with `folder` and
`description`; add tag `lesson` to record a mistake), `nt_done`, `nt_update`, `nt_tag`, `nt_mv`, `nt_archive` (retire
stale notes, reversible), `nt_supersede`/`nt_relink` (reconcile duplicates/obsolete notes); **retrieve:** `nt_index` (start here — a compact
catalog of note stubs plus the active tasks, no bodies), `nt_get` (fetch one
note's full body by id/slug/title, optional `section`), `nt_ready`,
`nt_status` (one-call project/area state), `nt_view` (recall the user's saved
smart views — list them by calling it bare), `nt_log`, `nt_search` (ranked
stubs, text and/or tag; `full:true` inlines bodies), `nt_recall` (lessons-first,
paraphrase-aware retrieval for a free-text task context — surfaces past mistakes
before you repeat them), `nt_links` (forward links + backlinks). They go through the same locked, journaled engine as the CLI,
default `source` to `claude`, and require **stable task ids** (positional
`task:N` is refused — the index isn't safe for an agent). Retrieval is
index-first progressive disclosure: load the small stub catalog, then fetch
bodies on demand.

### Parallel agents — workstreams

When several agents share one store at once (parallel git worktrees, CI jobs, web
sessions), set **`NT_WORKSTREAM`** in each agent's environment to keep their
in-flight **tasks** isolated while **notes** (the knowledge base) stay shared:

```jsonc
// per-worktree MCP registration
{ "mcpServers": { "nt": { "type": "stdio", "command": "/abs/path/to/nt",
  "args": ["mcp"], "env": { "NT_WORKSTREAM": "auto" } } } }
```

- A **literal** value (`"NT_WORKSTREAM": "feat-x"`) names the workstream — the
  most robust choice, and what a harness/CI should export. **`auto`** instead
  derives the id from the git branch checked out in the **MCP server process's
  working directory** (falling back to that directory's basename) — convenient
  for worktree-per-process setups like grove, where each `nt mcp` runs in its own
  worktree. Avoid `auto` when one server is shared across trees, or the branch may
  be renamed mid-session; prefer a literal there.
- `nt_add` stamps the resolved id (`ws:` on the task); `nt_index` / `nt_ready` /
  `nt_status` / `nt_log` scope to it. Tasks with no workstream (the human's
  CLI/TUI/web backlog) stay visible to every agent — only *another* agent's
  stamped tasks are hidden. `nt_search` / `nt_view` are never scoped.
- A read can pass `workstream: "*"` to see every workstream, or an explicit id to
  target another. **Unset → no isolation**, identical to single-agent behavior.

`nt_add` titles are meant to be **short and scannable** — one actionable line,
verb-first, ~10 words / 60 chars. Put detail in the task's **body**: `nt_add`
takes a `body` arg, saved as the task's linked note so the title stays clean and
the detail is one click away (the web shows a 📄 details chip; following the task
opens it). Only genuine paragraph-length `text` with no `body` (≥240 chars) is
auto-split the same way; ordinary verbose one-liners are left intact and just
clamp to a few lines in the UI (full text on hover / on edit).

These machine-created task notes are filed under **`notes/__tasks__/`** (the
reserved-looking name avoids colliding with a plain `tasks` folder you might keep
for your own notes; daily notes likewise go under `notes/journal/`), so they stay
grouped and don't clutter a human's hand-curated folders.

Hook, skill, and MCP compose: the hook mirrors the todo list automatically, the
skill/MCP capture notes and read back context. Use the MCP server if your client
supports it; the CLI + skill work everywhere.

## Hook vs. skill — when each fires

- **Hook** = passive, automatic. Mirrors Claude's *own* todo list. Best for
  capturing the agent's working task list without asking.
- **Skill** = active, on request. For deliberately saving notes, reading back prior
  context, or managing tasks conversationally — things that aren't in the todo
  list.

They compose: the hook keeps the task list in sync; the skill handles notes,
read-back, and ad-hoc edits.

---

## The loop, end to end

```bash
# session 1 — Claude works, todos sync automatically via the hook
#   (or: nt add "fix token refresh race" --source claude)

# session 2 — pick up where it left off
nt ready --json                    # what's actionable right now (open, unblocked)
nt index --source claude --json    # the fuller context: stub catalog of everything captured
nt show token-refresh-race         # fetch a specific note's body on demand
# → Claude reads its prior work back and continues
```

That pickup step is the whole point: the action items don't vanish when the
session ends — and `nt ready` tells the next agent exactly where to start.
