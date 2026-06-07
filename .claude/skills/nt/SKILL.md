---
name: nt
description: Capture and recall tasks and notes in the user's nt store — durable memory that survives across AI sessions. Use when the user asks to save/track an action item or TODO, take a note, mark something done, or recall what was captured in earlier sessions; also when the user types /nt. nt stores everything as plain files (todo.txt tasks + markdown notes), so action items you create here outlive the session.
---

# nt — durable task & note memory for AI sessions

`nt` is a local, file-backed task and note manager. Its purpose is to be the
**memory layer for AI coding sessions**: action items and notes you capture here
persist as plain text that the user — and the next session — can read back.

Everything is the `nt` CLI. Always pass `--source claude` so AI-created items are
distinguishable from what the user typed by hand.

## Start here: `nt ready`

At the start of substantive work, find what's actionable and reload prior context:

```bash
nt ready --json                     # open, UNBLOCKED tasks by urgency — start here
nt recall --source claude --json    # the fuller context: tasks + notes you created before
```

`nt ready` is your "pick up here" feed — it omits completed tasks and anything
still waiting on a dependency, so you act on work that's genuinely available.
`nt recall` is the broader read of everything captured. Lead with `ready`.

## Capture tasks

When you identify an action item, persist it instead of letting it vanish:

```bash
nt add "refactor auth middleware" --source claude --pri high --due today --tag backend --project api
```

Flags: `--pri high|med|low`, `--due today|tomorrow|fri|+3d|YYYY-MM-DD`,
`--tag NAME` (repeatable), `--project NAME`, `--blocks <id>` (this task blocks
another), `--discovered-from <id>` (see below), `--recur weekly|3d|…`,
`--note <slug>` (link to a note).

## Capture the *why*, not just the *what*

A todo list records what's left to do; durable memory also needs the reasoning a
future session would otherwise have to rediscover. Capture two things as you work:

- **Discovered work** — when you surface a *new* task while doing another, link it
  to its origin so the chain of work is recoverable:

  ```bash
  nt add "backfill user.tier column" --discovered-from <current-task-id> --source claude
  ```

  `nt links <id>` then shows "discovered from ↑" and "discovered here ↳" both ways.

- **Decisions, constraints, dead-ends** — when you make a non-obvious call or rule
  something out, write a note (not a task) so the *why* survives:

  ```bash
  nt note "Chose flock+atomic-rename over SQLite" \
    --body "Store must stay greppable/git-able; WAL would hide state. Tried X, too slow." \
    --source claude --tag decision
  ```

  Treat this like leaving a code comment for the next engineer — it's the highest-
  value memory, and `nt recall --json` now returns note bodies so the next session
  reads it back in full.

## Capture notes

For findings, context, or anything longer than a task line:

```bash
nt note "JWT tokens expire after 24h" --body "Refresh window is 7d. See auth.go." --source claude --tag auth
```

## Update / complete

```bash
nt done <id>                              # mark done (id is the short code from `nt list`)
nt update <id> --status doing --due +2d   # status: open|doing|blocked|done
```

## Link & search

```bash
nt links <id>            # forward links + backlinks for an item
nt search "race condition" [--type note|task]
```

Use `[[note-slug]]` or `[[<task-id>]]` inside task text or note bodies to
cross-link tasks and notes; backlinks are found automatically.

## Conventions

- **Always** `--source claude` on items you create.
- Prefer recalling before creating, to avoid duplicating an existing item.
- Tasks are one line; put anything longer in a note and link to it.
- The store is global at `$NT_DIR` (default `~/.local/share/nt`); `nt path` prints it.

## Automatic sync (optional)

If the user has wired the PostToolUse hook (`nt hook`, see
`docs/claude-integration.md`), your `TodoWrite` list is mirrored into nt
automatically — you don't need to also `nt add` those items.
