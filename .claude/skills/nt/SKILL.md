---
name: nt
description: Capture and recall tasks and notes in the user's nt store — durable memory that survives across AI sessions. Use when the user asks to save/track an action item or TODO, take a note, mark something done, search or recall what was captured before, or organize the knowledge base; also when the user types /nt. nt stores everything as plain files (todo.txt tasks + markdown notes in folders), so what you capture here outlives the session.
---

# nt — durable task & note memory for AI sessions

`nt` is a local, file-backed task and note manager. Its purpose is to be the
**memory layer for AI coding sessions**: action items and notes you capture here
persist as plain text — tasks in `tasks.txt`, notes as markdown in `notes/` (with
subfolders) — that the user and the next session can read back, `grep`, and open
in Obsidian.

Everything is the `nt` CLI. Always pass `--source claude` so AI-created items are
distinguishable from what the user typed by hand.

> If the `nt` MCP server is registered with your client, **prefer the typed
> `nt_*` tools over shelling out** — they go through the same store, default
> `source` to `claude`, and avoid CLI-string mistakes. Capture: `nt_add`,
> `nt_note`, `nt_done`, `nt_update`, `nt_tag`, `nt_mv`. Retrieve: `nt_ready`,
> `nt_recall`, `nt_search`, `nt_links`, `nt_log`. Fall back to the `nt` commands
> below when the tools aren't available — the workflow is identical.

## Start here: `nt ready` + `nt recall`

At the start of substantive work, find what's actionable and reload prior context:

```bash
nt ready --json                     # open, UNBLOCKED tasks by urgency — start here
nt recall --source claude --json    # the fuller context: tasks + notes you created before
```

`nt ready` is your "pick up here" feed; `nt recall` is the broader read. **Before
creating anything, retrieve first** (`nt recall` / `nt search`) so you don't
duplicate an item that already exists.

## Capture tasks

```bash
nt add "refactor auth middleware" --source claude --pri high --due today --tag backend --project api
```

Flags: `--pri high|med|low`, `--due today|tomorrow|fri|+3d|YYYY-MM-DD`,
`--tag NAME` (repeatable), `--project NAME`, `--blocks <id>`,
`--discovered-from <id>`, `--recur weekly|3d|…`, `--note <slug>` (link to a note).

## Capture notes — and file them into folders

For findings, context, decisions — anything longer than a task line:

```bash
nt note "JWT tokens expire after 24h" --body "Refresh window is 7d. See auth.go." --source claude --tag auth
```

**Notes live in folders, and you set the folder at capture** (it's created as
needed). Two equivalent forms — use them; don't dump everything in the root:

```bash
nt note "Auth design" --folder ref --source claude            # → notes/ref/auth-design.md
nt note "decisions/Chose flock over SQLite" --source claude   # path-style: text before the last "/" is the folder
```

The `nt_note` MCP tool takes the same **`folder`** argument. Common folders:
`ref/` (reference), `decisions/`, `inbox/` (triage later). Bare `[[name]]` links
resolve across folders by shortest path-suffix, so foldering never breaks
linking — and you can refile later with `nt mv`.

Set **structured frontmatter at capture** with `--field key=value` (repeatable):

```bash
nt note "Auth design" --folder ref --field status=stable --field area=auth --source claude
```

nt preserves any frontmatter it doesn't model (including properties added in
Obsidian), so capturing and curating never clobber the user's metadata.

## Capture the *why*, not just the *what*

Durable memory needs the reasoning a future session would otherwise rediscover:

- **Discovered work** — when you surface a *new* task while doing another, link it
  to its origin: `nt add "backfill user.tier column" --discovered-from <id> --source claude`.
  `nt links <id>` then shows "discovered from ↑" / "discovered here ↳" both ways.
- **Decisions, constraints, dead-ends** — write a note (not a task) so the *why*
  survives. Treat it like a code comment for the next engineer; `nt recall --json`
  returns note bodies so the next session reads it back in full.

## Find & navigate the knowledge base

```bash
nt search "race condition"                 # full-text over notes + tasks
nt search --tag auth --tag ref             # tag-filtered (AND); --tag alone lists, no query needed
nt search "jwt" --tag auth --type note     # combine text + tag, scope to note|task|all
nt tags                                    # the tag vocabulary with counts — keep it controlled
nt links <handle>                          # forward links + backlinks for a note or task
nt links --orphans                         # notes nothing links to — gaps in the graph to wire up
```

MCP equivalents: `nt_search` (query and/or tag), `nt_links` (handle). **Read links
before starting related work, not just when writing them** — `nt links <id>`
reconstructs why a task exists and surfaces the decisions and sibling work around
it, recovering reasoning a prior session left behind.

Use `[[note-slug]]` or `[[<id>]]` inside task text or note bodies to cross-link;
backlinks are found automatically.

## Curate (refile & retag)

Keep the KB tidy as it grows — no `$EDITOR` needed:

```bash
nt mv <note> ref/auth              # refile/rename, rewriting every [[link]] to it
nt tag <note> +reviewed -inbox     # add/remove tags
nt rm <note>                       # delete → .trash/ (refuses if inbound [[links]] would dangle; --force overrides)
```

MCP: `nt_mv` (handle, dest), `nt_tag` (handle, add, remove). All preserve other
frontmatter.

## Update / complete

```bash
nt done <id>                              # mark done (id is the short code nt prints)
nt update <id> --status doing --due +2d   # status: open|doing|blocked|done
```

## Handles

Every verb accepts the **same handle nt prints**: a note's slug/title or its
6-char short id, a task's short id. So you can capture with `nt note` and reuse
the returned id directly with `nt links` / `nt tag` / `nt mv` / `nt rm`.

## Conventions

- **Always** `--source claude` on items you create.
- Retrieve (`recall`/`search`) before creating, to avoid duplicates.
- Tasks are one line; put anything longer in a note and link to it.
- Organize notes into folders (`ref/`, `decisions/`, `inbox/`) rather than a flat root.
- The store is global at `$NT_DIR` (default `~/.local/share/nt`); `nt path` prints it.

## Automatic sync (optional)

If the user wired the PostToolUse hook (`nt hook`), your `TodoWrite` list is
mirrored into nt automatically — you don't need to also `nt add` those items.
