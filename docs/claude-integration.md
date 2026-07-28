# nt + Claude Code

`nt` is built to be the **durable memory layer for AI coding sessions** — the
place where the action items and notes an agent produces survive after the
session ends, in plain text the next session can read back. There are two
integration points: an automatic **hook** and an explicit **`/nt` skill**.

---

## 1. Automatic capture & recall — two PostToolUse hooks

`nt hook` is one command that dispatches on which tool fired it — wire it under
**both** matchers below and it handles each independently.

### 1a. Todo mirror (matcher `TodoWrite`)

Claude Code maintains a todo list via its `TodoWrite` tool. `nt hook` mirrors
that list into your nt store, idempotently, tagged `src:claude`.

### 1b. Error-triggered lesson recall (matcher `Bash`)

The same loop the OpenCode/Pi integrations run: when a bash command **fails**,
`nt hook` searches your recorded **lessons** (`nt note … --lesson`) for the
command + error tail. If it finds one, it feeds it back to Claude via the
hook's block+reason contract — the mistake summons its own antidote on the
next turn instead of relying on the agent remembering to run `nt recall`.

### Setup

Add both matchers to your Claude Code settings (`~/.claude/settings.json` for
all projects, or a project's `.claude/settings.json`):

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "TodoWrite",
        "hooks": [
          { "type": "command", "command": "nt hook" }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "nt hook" }
        ]
      }
    ]
  }
}
```

That's it. From then on:

- new todos are added to nt as tasks (`src:claude`),
- status changes are mirrored (`in_progress` → doing, `completed` → done),
- nothing is duplicated — a per-session map (`$NT_DIR/.claude-sync.json`) tracks
  which todo maps to which nt task,
- a failed bash command that matches a recorded lesson surfaces it on the next turn.

### How it behaves

| TodoWrite status | nt task |
|------------------|---------|
| `pending`        | open    |
| `in_progress`    | `s:doing` |
| `completed`      | done (`x`) |

`nt hook` reads the hook's JSON event from stdin and is silent by design in
every case except the one it's SUPPOSED to speak up for: a failed Bash command
matching a recorded lesson, where it exits **2** with the lesson(s) on stderr
(Claude Code's block+reason contract for feeding text back to the model).
Every other path — a successful command, a TodoWrite event, nothing relevant
on record, `nt` not installed, the store not opening — exits **0** silently. It
can never break or meaningfully slow your session either way.

> Tip: while the hook runs, keep the `nt` TUI open in a side pane (`nt`). Tasks
> appear within ~80ms (fsnotify) as Claude works — no window switching.

---

## 2. Explicit capture — the `/nt` skill

The bundled skill ([.claude/skills/nt/SKILL.md](../.claude/skills/nt/SKILL.md))
teaches Claude to use `nt` directly. With it installed, you can say things like:

- "what should I work on?" → Claude runs `nt ready` (open, unblocked, by urgency)
- "save that as a task in nt"
- "note this finding for later"
- "what did we capture last session?" → Claude runs `nt index --json`
- or just type `/nt`

Claude will run the right `nt` commands (`ready`, `add`, `note`, `index`,
`recall`, `show`, `done`, `links`, `search`), always passing `--source claude`.

**Start a session with `nt ready`.** It returns only actionable work — open
tasks that aren't done and aren't waiting on a dependency — newest-urgency
first. That's the agent's "pick up here" feed; `nt index` is the broader
"everything we captured" read — the tiered stub catalog: pinned standing notes
(`rules/`, `memory/`, `ref/`, and anything tagged `pin`) always listed, the
last 14 days of recent stubs, and per-folder counts for the rest (`--all` for
the flat catalog; scoped `--tag`/`--folder` views are complete) — plus the
active tasks, from which Claude fetches a specific body with
`nt show <handle>` on demand.

**Before each task, run `nt recall "<plain words>"`** — paraphrase-aware
retrieval with lessons flagged ⚑ first; it returns nothing when nothing is
relevant, so it's cheap to run every time.

Install it by keeping `.claude/skills/nt/` in your project, or copy it to
`~/.claude/skills/nt/` to make it available everywhere.

## 2b. The session loop — `/nt-learn` and `/nt-distill`

Two more bundled skills close the memory loop that Pi and OpenCode already
ship as `/learn` and `/distill`. They carry the same prompt bodies; only the
skill name differs, because `~/.claude/skills/` is a global namespace shared
with every other tool's skills and bare `learn`/`distill` are too generic to
claim there.

- **[`/nt-learn`](../.claude/skills/nt-learn/SKILL.md)** — harvest what should
  outlive the session: lessons, rules, core memory, notes, follow-up tasks. It
  dedups against the store first, proposes a numbered list, and writes nothing
  until you approve.
- **[`/nt-distill`](../.claude/skills/nt-distill/SKILL.md)** — the hygiene
  counterpart, in two passes. Pass 1 merges near-duplicate notes (via the
  read-only `nt_distill` tool). Pass 2 prunes the **always-loaded block** —
  `rules/` and `memory/` — looking for subsumption, contradictions, dead
  triggers, and rules that turned out not to be always-relevant. Both passes
  land in one approval list; nothing is merged, demoted, or retired silently.
  `/nt-distill rules` runs pass 2 alone.

Pass 2 matters most on Claude Code. Pi and OpenCode inject the rules block live
and nudge you when it exceeds `NT_INJECT_MAX`; here the block reaches Claude
through `CLAUDE.md` (see [Standing rules](#standing-rules-in-claudemd)), which
has no size warning at all — a prompt is the only signal you get. After pruning,
re-run `nt export --tag rule` or the change never reaches `CLAUDE.md`.

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

Tools exposed (**19**) — **capture:** `nt_add`, `nt_note` (with `folder`,
`description`, and `kind: lesson|decision|ref|rule|memory` — canonical tag +
folder; always give a `description`, it's what `nt_index` shows), `nt_note_edit`
(fix an EXISTING note in place — `append`/`body`/`old_string`+`new_string`/
`description`; no new id, unlike `nt_note supersede:`), `nt_update` (status:"done" completes; the response echoes what `changed`), `nt_rm` (remove a
mistaken task — journaled, `nt undo` restores), `nt_tag`, `nt_mv`, `nt_archive` (retire
stale notes — set `superseded_by` to reconcile duplicates), `nt_relink` (fix a wrong outbound link); **retrieve:** `nt_index` (start here — a compact
catalog of note stubs plus the active tasks and recent completions — tiered on
large stores: pinned standing notes + recent stubs + folder counts; blocked
tasks listed separately; no bodies), `nt_get` (fetch one
note's full body by id/slug/title, optional `section`),
`nt_status` (one-call project/area state: in-progress + blocked + open-by-urgency
+ recently done), `nt_view` (recall the user's saved
smart views — list them by calling it bare), `nt_search` (ranked
stubs, text and/or tag; `full:true` inlines bodies), `nt_recall` (lessons-first,
paraphrase-aware retrieval for a free-text task context — surfaces past mistakes
before you repeat them), `nt_links` (forward links + backlinks); **health:**
`nt_doctor` (read-only store hygiene — dangling links, task-file issues, expired
notes), `nt_distill` (read-only — every near-duplicate note pair, uncapped, for
a human-gated merge), `nt_mindmap` (a note's outline + wikilinks as a graph). They go through the same locked, journaled engine as the CLI,
default `source` to `claude`, and require **stable task ids** (positional
`task:N` is refused — the index isn't safe for an agent). Retrieval is
index-first progressive disclosure: load the small stub catalog, then fetch
bodies on demand. `nt_add`/`nt_note` are dedup-advisory: they always create and
return a `similar` list when near-duplicates exist so the agent can consolidate
instead of forking memory (the CLI `nt note` is stricter — it refuses near-dups
outright with repair commands).

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
- `nt_add` stamps the resolved id (`ws:` on the task); `nt_index` / `nt_status` /
  `nt_search` scope their *task* results to it. Tasks with no workstream (the
  human's CLI/TUI/web backlog) stay visible to every agent — only *another*
  agent's stamped tasks are hidden. Notes and `nt_view` are never scoped.
- A read can pass `workstream: "*"` to see every workstream (each task then
  carries its `workstream` so you can tell whose is whose), or an explicit id to
  target another. **Unset → no isolation**, identical to single-agent behavior.
  The CLI scopes the same way when `NT_WORKSTREAM` is set
  (`list`/`ready`/`log`/`review`/`index`), and `nt undo`/`redo` refuse another
  workstream's change unless `--force`.

`nt_add` titles are meant to be **short and scannable** — one actionable line,
verb-first, ~10 words / 60 chars. Put detail in the task's **body**: `nt_add`
takes a `body` arg, saved as the task's linked **detail note** — filed under
`notes/__tasks__/`, and **appended to on re-use** (a second `body` for the same
task extends the same note rather than creating another) — so the title stays
clean and the detail is one click away (the web shows a 📄 details chip;
following the task opens it, and `nt show <task-id>` renders the task and its
detail note together). Only genuine paragraph-length `text` with no `body`
(≥240 chars) is auto-split the same way; ordinary verbose one-liners are left
intact and just clamp to a few lines in the UI (full text on hover / on edit).

These machine-created task notes are filed under **`notes/__tasks__/`** (the
reserved-looking name avoids colliding with a plain `tasks` folder you might keep
for your own notes; daily notes likewise go under `notes/journal/`), so they stay
grouped and don't clutter a human's hand-curated folders.

Hook, skill, and MCP compose: the hook mirrors the todo list automatically, the
skill/MCP capture notes and read back context. Use the MCP server if your client
supports it; the CLI + skill work everywhere.

## Standing rules in CLAUDE.md

Notes in `rules/` (tag `rule`) and `memory/` (tag `memory-core`, written with
`--kind memory`) are the **always-loaded layers**: they're pinned atop every
`nt index` so an agent sees them before anything else, and they compile into
your `CLAUDE.md` / `AGENTS.md` with `nt export --tag rule`. Keep them small —
they're the part of the store that's paid for on every request.

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
nt index --json                    # the tiered catalog: pinned standing notes + recent stubs + folder counts
nt recall "token refresh race"     # lessons flagged ⚑ first, paraphrase-aware — before touching the code
nt show <task-id>                  # the task + its detail note, rendered together
nt note "Chose flock over SQLite" --kind decision --description "one writer at a time, no CGo"   # capture as you go
# → Claude reads its prior work back and continues
```

That pickup step is the whole point: the action items don't vanish when the
session ends — and `nt ready` tells the next agent exactly where to start.
