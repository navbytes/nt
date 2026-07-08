---
name: nt
description: Capture and recall durable memory in the user's nt store across OpenCode sessions. Use when asked to save/track a task or TODO, take a note, record a decision or finding, mark something done, search or look up what was captured before, or organize the knowledge base. nt persists everything as plain files (tasks + markdown notes) that outlive the session.
compatibility: opencode
metadata:
  backend: nt
---

# nt — durable memory for OpenCode

`nt` is the user's local task + note store and the **memory backend** for this
OpenCode setup. What you capture survives the session as plain text the next
session reads back. Drive it through the **`nt_*` MCP tools** (registered via `nt
mcp install --client opencode`); fall back to the `nt` CLI only if the tools are
absent.

## The loop — index first, fetch on demand

Don't bulk-load the store — load a cheap **index**, then open only what's
relevant. (Dumping every note body wastes context and *degrades* reasoning.)

**Starting substantive work:**

- `nt_index` — the KB catalog: one stub per note (id · title · one-line
  description · tags · folder), no bodies, plus active tasks. Read it first.
  Prefer `format:"compact"` (far fewer tokens). Large stores return a tiered
  catalog (pinned `rules/`/`memory/`/`ref/`/`pin` notes + last 14 days, older as
  per-folder counts) — expand with `folder` (`"."` = root) or `all:true`.
- `nt_status` — in-progress + blocked + open-by-urgency tasks, recent
  completions, linked notes.

**Before a task — surface past lessons:**

- `nt_recall` — pass plain-words `context` of what you're about to do; get the
  most relevant notes back, **lessons/gotchas first**, even when your wording
  differs (it stems + expands synonyms, unlike `nt_search`'s exact match). A
  `lesson:true` result is a past mistake — `nt_get` it and heed it before coding.
  Call it at task start, not just when stuck. Your workstream's project gets a
  soft boost (`project:"none"` disables); an empty result means nothing recorded
  is on-topic — proceed.

**Specifics, on demand:**

- `nt_search` — ranked stubs matching text/tag (title matches first; `limit`
  default 8; `truncated:true` = narrow the query). Returns id + snippet. Use it
  for *known* terms; use `nt_recall` for a fuzzy task description.
- `nt_get` — one note's full body by id (or a `section` heading). How you read a
  note after the index/search points you at it.
- `nt_links` — forward links + backlinks, to reconstruct why something exists.

**As you work**, capture the *why*, not just the *what*:

- `nt_add` — a task. Short verb-first title (~60 chars); detail in `body`. Chain
  discovered work with `discovered_from:<id>`.
- `nt_note` — a finding/decision/constraint/dead-end. **Always set
  `description`** (one line) — it's what `nt_index` shows. The body is what a
  future `nt_get` reads back.
- `nt_update` — change a task by its **stable id** (`status:"done"` completes it).

## Where things go (folders + tags)

Two folders/tags are reserved for the **always-in-context** layer the plugin
injects every session — keep them small and high-signal:

| Put it here | Folder | Tag | When |
|-------------|--------|-----|------|
| **Rule** (stable directive: "always run gofmt", style/process) | `rules/` | `rule` | Every session. Billed every turn — keep terse. |
| **Core memory** (small evolving fact: a preference, a key convention) | `memory/` | `memory-core` | The agent should *always* know it. A handful, not hundreds. |
| **Knowledge base** (findings, decisions, reference) | `ref/`, `decisions/` | topical, e.g. `auth` | Looked up on demand (`nt_index` → `nt_search`/`nt_get`) — **not** injected, so size is free. |
| **Lesson** (a mistake/footgun/dead-end not to repeat) | `lessons/` | `lesson` | Surfaced by `nt_recall` at task start — **not** injected, so free until relevant. Capture with `nt_note kind:"lesson"` (CLI `nt note … --lesson`). |

So: durable directive → `nt_note kind:"rule"`; recorded mistake → `kind:"lesson"`
(trigger in the description: "when X, do Y — not Z"); decisions/reference →
`kind:"decision"`/`"ref"`; learned preference → `kind:"memory"`; everything else
→ a foldered note found later by `nt_search`.

Use stable ids and `[[slug]]`/`[[id]]` links to cross-reference; backlinks
resolve automatically. Always pass `source:"opencode"` on `nt_add`/`nt_note` (the
tools default to `"claude"`) so your items are distinct from the user's.

## Curate

- `nt_mv` — refile/rename a note (rewrites every `[[link]]`).
- `nt_tag` — add/remove tags (e.g. promote a `ref` note to `rule` once stable).
- `nt_note_edit` — fix a note in place: `append`, `body` (replace wholesale), or
  `old_string`+`new_string` (patch ONE exact match — refuses if not unique). No
  new id, unlike `nt_note supersede:`. Combine with `description` to update the
  summary too.
- `nt_archive` — retire a stale note from index/search/recall (`undo:true`
  restores). `superseded_by:<id>` leaves a pointer to the replacement.
- `nt_relink` (from, to) — repoint every `[[link]]` (e.g. after superseding).
- `nt_rm` — permanently remove a mistaken/duplicate task by id (journaled; `nt
  undo` restores). CLI: `nt gc` reclaims superseded stubs + stranded task notes
  >30d → `.trash/` (dry-run by default; `--yes` applies); `nt edit <id>
  --append/--desc/--body/--old-string+--new-string` is the CLI `nt_note_edit`.

**Dedup signal:** `nt_note` always creates; when near-duplicates exist the
response carries a `similar` list — check it, and if you truly doubled a note,
consolidate (`nt_archive superseded_by=<kept id>`) or extend the original. (The
CLI, by contrast, refuses near-duplicates unless `--force`/`--supersede`.) A
`danglingLinks` entry is a `[[link]]` typo to fix.

## Conventions

- **Retrieve before you create** (`nt_index`/`nt_search`) to avoid duplicates.
- Always give notes a **`description`** so the index stays scannable.
- Tasks are one line; anything longer is a note `body`, linked from the task.
- Keep the `rules/`+`memory/` core **small** — billed every request. The big KB
  belongs behind `nt_search`/`nt_get`, free until used.
- Promote, don't duplicate: retag a `ref` note into `rule` rather than copying it.
- `nt doctor` checks store health (dangling `[[links]]`, missing descriptions) —
  run it occasionally.
