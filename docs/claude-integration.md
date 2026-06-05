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

- "save that as a task in nt"
- "note this finding for later"
- "what did we capture last session?" → Claude runs `nt recall --source claude`
- or just type `/nt`

Claude will run the right `nt` commands (`add`, `note`, `recall`, `done`,
`links`, `search`), always passing `--source claude`.

Install it by keeping `.claude/skills/nt/` in your project, or copy it to
`~/.claude/skills/nt/` to make it available everywhere.

---

## Hook vs. skill — when each fires

- **Hook** = passive, automatic. Mirrors Claude's *own* todo list. Best for
  capturing the agent's working task list without asking.
- **Skill** = active, on request. For deliberately saving notes, recalling prior
  context, or managing tasks conversationally — things that aren't in the todo
  list.

They compose: the hook keeps the task list in sync; the skill handles notes,
recall, and ad-hoc edits.

---

## The loop, end to end

```bash
# session 1 — Claude works, todos sync automatically via the hook
#   (or: nt add "fix token refresh race" --source claude)

# session 2 — recall what was captured
nt recall --source claude --json
# → Claude reads its prior action items back and continues where it left off
```

That recall step is the whole point: the action items don't vanish when the
session ends.
