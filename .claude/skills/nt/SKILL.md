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

Everything is the `nt` CLI. Pass `--source claude` on **write commands**
(`add`, `note`, `update`) so AI-created items are distinguishable from what the
user typed by hand.

> If the `nt` MCP server is registered with your client, **prefer the typed
> `nt_*` tools over shelling out** — they go through the same store, default
> `source` to `claude`, and avoid CLI-string mistakes. Capture: `nt_add`,
> `nt_note` (`kind` files it), `nt_note_edit` (fix an EXISTING note in place —
> `append`/`body`/`old_string`+`new_string`/`description` — no new id, unlike
> `nt_note supersede:`), `nt_update` (status:"done" completes), `nt_tag`,
> `nt_mv`, `nt_archive` (supersede), `nt_relink`, `nt_rm` (tasks). Retrieve:
> `nt_index`, `nt_search`, `nt_recall`, `nt_get` (handle or id), `nt_status`,
> `nt_links`, `nt_view`. Fall back
> to the `nt` commands below when the tools aren't available — the workflow is identical.

## Start here: `nt index` + `nt ready`

At the start of substantive work, load a cheap catalog of what exists — then open
only what's relevant. Don't bulk-load note bodies (it wastes context and degrades
reasoning).

```bash
nt index --json                     # KB catalog: note stubs (id·title·description) + active tasks — no bodies
nt index --since 14d --json         # what's new since last session (also: today | YYYY-MM-DD)
nt ready --json                     # open, UNBLOCKED tasks by urgency
```

On large stores the index is **tiered**: pinned standing notes (rules/, memory/,
ref/, or tag `pin`) + everything changed in the last 14 days, with the older
remainder as per-folder counts. Expand a folder with `--folder <f>`, or pass
`--all` for every stub. **`--project <name>` hard-filters to that project**
(via `project:` frontmatter or `+project` tag) — unlike `recall --project`,
which is a soft ranking preference. Standing knowledge belongs in the pinned
layers — file it with `--kind rule|ref` (or tag `pin`) so every future session
sees it.

`nt index` is your "what's here" catalog; `nt ready` is the task feed. **Before
creating anything, retrieve first** (`nt index` / `nt search`) so you don't
duplicate an item that already exists. If `nt note` refuses with "a
near-duplicate already exists", follow its hint: `nt edit <id> --append`, or
`--supersede <id>`, or rerun with `--force` only if genuinely distinct. (The MCP
`nt_note` instead always creates and returns a `similar` list — consolidate
afterwards.) To read a specific note — or a task and its linked detail —
`nt show <id>` (MCP: `nt_get`); to find one, `nt search <q>` returns ranked stubs.

## Before a task: `nt recall` — don't repeat past mistakes

When you're about to start something, describe it to `nt recall` and read what a
past session learned — **recorded lessons surface first**, even when your wording
differs (it's paraphrase-aware, unlike substring `nt search`):

```bash
nt recall "adding concurrent token refresh"    # MCP: nt_recall(context: "...")
nt recall --json "adding concurrent token refresh"   # JSON adds confidence fields
nt recall --explain "adding concurrent token refresh"   # term-by-term scoring + why notes were dropped
nt recall --explain-note <id>      # explain one note's score (or why it scored zero)
```

An empty result usually means nothing relevant is recorded — proceed. **Each
hit shows a confidence tier** `[strong 4/4]`, `[medium 2/4]`, `[weak 1/4]` —
the tier and concept coverage (how many of your query's ideas matched). **Read
the tier, not any internal score** — the tier is normalized across queries; a
raw score is query-dependent and not comparable. When the top hit is weak, a
banner warns you. If your query was long and oddly specific, one **shorter**
retry is worth it: recall weighs distinct concepts a note shares with you, so
wordiness can dilute meaning. Retry shorter, not looser.

When `NT_WORKSTREAM` is set, your own project's notes get a soft ranking
preference in results — cross-project results stay visible below. Override
with `--project <name>` or disable with `--project none`.

When you hit a mistake, footgun, or dead-end, capture it as a **lesson** so the
next session recalls it — put the trigger in the description ("when X, do Y — not Z"):

```bash
nt note "single-flight the refresh; parallel calls double-spend" --lesson \
  --project tripto --source claude          # ALWAYS a --project or --tag; see below
nt recall --lessons-only                     # bare: list every recorded lesson
```

**Always give a lesson a `--project` or a topical `--tag`.** This is not
housekeeping — it is what keeps the duplicate guard alive. `--lesson` applies
only the *structural* `lesson` tag, which the near-duplicate check strips before
comparing; with nothing else on the note its tag set is empty, the check can
never fire, and nt will silently accept a near-copy of a lesson you already
have. A store captured without topical tags fragments into many one-line notes
saying the same thing, and neither `nt note` nor `nt distill` will tell you.

**Precision note:** recall returns the top-8 results by default (top-N, no hard
precision guarantee). A very unrelated query can still return results that look
confident — always check the **tier** before trusting a hit. There is an internal
precision floor for multi-concept queries, but it's loose by design: better to
surface a weak match you dismiss than to hide something that needed finding.

## Capture tasks

```bash
nt add "refactor auth middleware" --source claude --pri high --due today --tag backend --project api
nt add "fix refresh race" --source claude --body "repro: two parallel calls…"   # detail → auto-linked note under notes/__tasks__/ (re-using --body on the same task appends)
```

Keep the title short and verb-first; put detail/reasoning/steps in `--body` —
it's saved as the task's linked note. Flags: `--pri high|med|low`,
`--due today|tomorrow|fri|+3d|YYYY-MM-DD`, `--tag NAME` (repeatable; `--tags a,b`),
`--project NAME`, `--blocked-by <id>` (this task waits for `<id>`), `--blocks <id>`
(the reverse; pass `none` to `--blocks` on the blocking task to clear an edge),
`--discovered-from <id>`, `--recur weekly|3d|…`, `--note <slug>`
(link an existing note; also on `nt update` to attach one later).

## Capture notes — and file them into folders

For findings, context, decisions — anything longer than a task line:

```bash
nt note "JWT tokens expire after 24h" --description "Access 24h, refresh 7d — see auth.go" --body "Refresh window is 7d. See auth.go." --source claude --tag auth
```

**Bodies with backticks, `$()`, or multiple lines: never inline them in the shell
string** — the shell eats them silently. Write the body to a temp file (or pipe
it) and use `--body-file`:

```bash
nt note "Decision: flock over SQLite" --kind decision --body-file /tmp/body.md --source claude
printf '%s\n' "the body…" | nt note "…" --body-file - --source claude
```

**File notes by class with `--kind lesson|decision|ref|rule|memory`** — it applies the
canonical tag + folder (`lessons/`, `decisions/`, `ref/`, `rules/`, `memory/`) so every
session converges on one layout (`--kind memory` files under `memory/` with tag
`memory-core` — the always-loaded core-memory layer). An explicit `--folder` (or path-style
`nt note "decisions/Chose flock"`) still wins for bespoke foldering. Bare
`[[name]]` links resolve across folders by shortest path-suffix, so foldering
never breaks linking — and you can refile later with `nt mv`. The `nt_note` MCP
tool takes the same **`kind`**/**`folder`** arguments.

To fix or extend a note later **without an editor**: `nt edit <note> --append
"Resolved in commit abc123"`, `--append-file notes.md` (or `-` for stdin — use
this for multi-line/backtick appends), `--body <text>` (replace the whole body
inline, no temp file needed), `--body-file new.md` (replace the body from a
file — for long/multi-line content), `--old-string "..." --new-string "..."`
(patch ONE exact match in place — the targeted fix for a longer note; refuses
if the match isn't unique, so make it longer to disambiguate), or `--desc "…"`
(set the one-line summary). These are mutually exclusive per call — pick one
way to change the body. MCP: `nt_note_edit` takes the same
`append`/`body`/`old_string`+`new_string`/`description` fields; it's the
in-place counterpart to `nt_note`, which only ever creates (`supersede:` mints
a *new* id rather than editing in place).

Tag a note with **`--project <name>`** when it's specific to one codebase in a
shared multi-project store — `nt recall --project <name>` (default: your
`NT_WORKSTREAM`) then ranks it above cross-project noise:

```bash
nt note "webhookd retry backoff" --kind decision --project webhookd --description "exponential, cap 5 tries"
```

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
  survives. Treat it like a code comment for the next engineer; give it a
  `--description` so it's findable in `nt index`, and the next session reads the
  full body back with `nt show <id>` (MCP: `nt_get`).

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
nt rm <task-id> --yes              # delete a task (agents must pass --yes; journaled, nt undo restores)
nt doctor                          # store health: dangling [[links]], near-duplicates, oversized pinned tier
nt archive <note>                  # retire a stale note from index/search/recall (reversible)
nt supersede <old> --by <new>      # (or nt note … --supersede <old>) — replace a note; the old one retires with a pointer
nt gc                              # plan: superseded stubs + stranded task notes >30d old
nt gc --yes                        # reclaim them → .trash/ (recoverable)
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
- Finished work gets `nt done`, never `nt rm` — done tasks feed the Logbook
  (`nt log`) that the next session reads; rm erases the history.
- Retrieve (`index`/`search`) before creating, to avoid duplicates.
- Tasks are one line; put anything longer in a note and link to it.
- Always give notes a `--description` — it's the one-line summary `nt index`
  shows; `nt show` prints it and export folds it in.
- File notes by class with `--kind` (`lessons/`, `decisions/`, `ref/`, `rules/`,
  `memory/`); use bespoke folders (`--folder`) only when no kind fits.
- The store is global at `$NT_DIR` (default `~/.local/share/nt`); `nt path` prints it.

## Workstreams (parallel sessions, shared store)

When several agents share one store (e.g. parallel git worktrees), `NT_WORKSTREAM`
**scopes tasks only** — your in-flight work doesn't mix with another session's.
**Notes always stay shared** across all agents, never scoped. Set via environment
(grove/CI/harness export it; `auto` derives it from git branch, falling back to
working directory basename — prefer a literal id). The MCP tools and CLI `nt add`
record it; `nt_index`/`nt_status` then scope to it.

- Tasks with no workstream (the human's CLI/TUI/web backlog) stay visible to
  everyone — only *another* agent's stamped tasks are hidden.
- Notes and `nt_search` are never scoped — knowledge and discovery are store-wide.
- `nt recall --project` defaults to `NT_WORKSTREAM` as a soft ranking preference
  (cross-project results stay visible below; pass `none` to disable).
- Pass `workstream: "*"` on a read to see every workstream's tasks; pass an
  explicit `workstream` to target another one. With `NT_WORKSTREAM` unset there
  is no scoping and behavior is unchanged.
- `nt undo` is workstream-safe: it refuses to revert a change made by another
  workstream (rerun with `--force` only if you truly mean it), prints exactly
  what it touched, and `nt redo` re-applies the last undo.

## Automatic sync (optional)

If the user wired the PostToolUse hook (`nt hook`), your `TodoWrite` list is
mirrored into nt automatically — you don't need to also `nt add` those items.
