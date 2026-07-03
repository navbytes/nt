---
name: nt
description: Capture and recall durable memory in the user's nt store across OpenCode sessions. Use when asked to save/track a task or TODO, take a note, record a decision or finding, mark something done, search or look up what was captured before, or organize the knowledge base. nt persists everything as plain files (tasks + markdown notes) that outlive the session.
compatibility: opencode
metadata:
  backend: nt
---

# nt — durable memory for OpenCode

`nt` is the user's local task + note store and the **memory backend** for this
OpenCode setup. Notes and tasks you capture here survive after the session ends,
as plain text the next session reads back. Drive it through the **`nt_*` MCP
tools** (registered via `nt mcp install --client opencode`); fall back to the
`nt` CLI only if the tools aren't present.

## The loop — index first, then fetch on demand

Don't bulk-load the whole store. Load a cheap **index** of what exists, then open
only what's relevant. (Dumping every note body wastes context and *degrades*
reasoning — long, irrelevant context measurably hurts.)

**At the start of substantive work:**

- `nt_index` — the KB catalog: one stub per note (id · title · one-line
  description · tags · folder) with NO bodies, plus the active task list. Read
  this first to see what's available. Prefer `format:"compact"` (same catalog,
  far fewer tokens). Large stores return a tiered catalog: pinned standing notes
  (`rules/`, `memory/`, `ref/`, tag `pin`) + the last 14 days, older notes as
  per-folder counts — expand with the `folder` filter (`"."` = root) or
  `all:true`. Blocked tasks are listed separately.
- `nt_status` — in-progress + blocked + open-by-urgency tasks, recent completions, linked notes.

**Before starting a task — surface past lessons:**

- `nt_recall` — pass a plain-words `context` of what you're about to do; get the
  most relevant notes back, **recorded lessons/gotchas first**, even when your
  wording differs from theirs (it stems + expands synonyms, unlike `nt_search`'s
  exact substring match). A result with `lesson:true` is a mistake a past session
  hit — `nt_get` it and heed it before writing code. This is the proactive half of
  the learn-from-sessions loop; call it at task start, not just when stuck.
  Your workstream's project gets a soft ranking boost (`projectMatch:true`;
  `project:"none"` disables); an empty result means nothing recorded is
  on-topic — proceed.

**When you need specifics (on demand):**

- `nt_search` — ranked stubs matching text/tag (title matches first; capped by
  `limit`, default 8; `truncated: true` means narrow the query). Returns
  id + snippet, not bodies. Use it for *known* terms; use `nt_recall` when you want
  relevant lessons for a fuzzy task description.
- `nt_get` — the full body of ONE note by id (or just a `section` heading). This
  is how you read a note after the index/search points you at it.
- `nt_links` — follow forward links + backlinks to reconstruct why something exists.

**As you work**, capture the *why*, not just the *what*:

- `nt_add` — a task. Short, verb-first title (~60 chars); put detail in `body`.
  Chain discovered work with `discovered_from: <id>`.
- `nt_note` — a finding, decision, constraint, or dead-end. **Always set
  `description`** (a one-line summary) — it's what `nt_index` shows, so the note
  is findable without opening it. The body is what a future `nt_get` reads back.
- `nt_update` — change a task by its **stable id** (status:"done" completes it)
  (never a row number).

## Where things go (folders + tags)

This setup reserves two folders/tags for the **always-in-context** layer the
plugin injects into every session — keep them small and high-signal:

| Put it here | Folder | Tag | When |
|-------------|--------|-----|------|
| **Rule** (stable directive: "always run gofmt", style/process) | `rules/` | `rule` | Must apply every session. Costs tokens every turn — keep terse. |
| **Core memory** (small evolving fact: a user preference, a key project convention) | `memory/` | `memory-core` | The agent should *always* know it. A handful, not hundreds. |
| **Knowledge base** (findings, decisions, reference) | `ref/`, `decisions/` | topical, e.g. `auth` | Looked up on demand via `nt_index` → `nt_search`/`nt_get` — **not** injected, so size is free. |
| **Lesson** (a mistake, footgun, or dead-end not to repeat) | `lessons/` | `lesson` | Surfaced by `nt_recall` at task start — **not** injected, so it costs nothing until relevant. Capture with `nt_note kind:"lesson"` (CLI: `nt note … --lesson`). |

So: a durable directive → `nt_note` with `kind:"rule"`; a recorded mistake →
`kind:"lesson"` (trigger in the description: "when X, do Y — not Z");
decisions/reference → `kind:"decision"` / `kind:"ref"`; a learned preference →
`kind:"memory"` (files under `memory/` with tag `memory-core`); everything else →
a normally-foldered note found later by `nt_search`.

Use stable nt ids and `[[slug]]` / `[[id]]` links to cross-reference; backlinks
resolve automatically. Always pass `source:"opencode"` explicitly on
`nt_add`/`nt_note` — the MCP tools default `source` to `"claude"` — so what you
create is distinguishable from the user's hand-entered items.

## Curate

- `nt_mv` — refile/rename a note (rewrites every `[[link]]`).
- `nt_tag` — add/remove tags (e.g. promote a `ref` note into `rule` once it's stable).
- `nt_note_edit` — fix an EXISTING note in place: `append` (add to the end),
  `body` (replace it wholesale), or `old_string`+`new_string` (patch ONE exact
  match — refuses if it's not unique, so include more context to disambiguate).
  No new id, unlike `nt_note supersede:`, which retires the old note. Combine
  with `description` to also update the one-line summary.
- `nt_archive` — retire a stale note from index/search/recall (reversible;
  `undo:true` restores). Pass `superseded_by:<id>` when another note replaces
  it — the old one leaves the index with a pointer.
- `nt_relink` (from, to) — repoint every `[[link]]` from one handle to another
  (e.g. after superseding, redirect references to the canonical note).
- `nt_rm` — permanently remove a mistaken/duplicate task by stable id (journaled;
  `nt undo` restores). CLI `nt gc` reclaims superseded stubs and stranded task
  notes >30d → `.trash/` (dry-run by default; `--yes` applies); CLI equivalent
  of `nt_note_edit` is `nt edit <id> --append/--desc/--body/--body-file/
  --old-string+--new-string`.

**Dedup signal:** `nt_note` always creates the note; when near-duplicates exist
the response carries a `similar` list — check it, and if you truly doubled an
existing note, consolidate (`nt_archive` with `superseded_by=<kept id>`) or
extend the original instead of leaving both. (The nt CLI, by contrast, refuses
near-duplicates unless `--force`/`--supersede`.) Watch the `danglingLinks` field
in the result — a `[[link]]` that didn't resolve is a typo to fix.

## Conventions

- **Retrieve before you create** (`nt_index` / `nt_search`) to avoid duplicates.
- Always give notes a **`description`** so the index stays scannable.
- Tasks are one line; anything longer is a note `body`, linked from the task.
- Keep the `rules/` + `memory/` core **small** — it's billed on every request.
  The big knowledge base belongs behind `nt_search`/`nt_get`, free until used.
- Promote, don't duplicate: when a `ref` note becomes a standing rule, retag it
  rather than copying it into the rules core.
- `nt doctor` checks store health (dangling `[[links]]`, notes missing a
  description); run it occasionally to keep the KB clean.
