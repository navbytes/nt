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
  this first to see what's available.
- `nt_ready` — open, unblocked tasks by urgency, if you only need the task feed.

**Before starting a task — surface past lessons:**

- `nt_recall` — pass a plain-words `context` of what you're about to do; get the
  most relevant notes back, **recorded lessons/gotchas first**, even when your
  wording differs from theirs (it stems + expands synonyms, unlike `nt_search`'s
  exact substring match). A result with `lesson:true` is a mistake a past session
  hit — `nt_get` it and heed it before writing code. This is the proactive half of
  the learn-from-sessions loop; call it at task start, not just when stuck.

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
- `nt_done` / `nt_update` — complete or change a task by its **stable id**
  (never a row number).

## Where things go (folders + tags)

This setup reserves two folders/tags for the **always-in-context** layer the
plugin injects into every session — keep them small and high-signal:

| Put it here | Folder | Tag | When |
|-------------|--------|-----|------|
| **Rule** (stable directive: "always run gofmt", style/process) | `rules/` | `rule` | Must apply every session. Costs tokens every turn — keep terse. |
| **Core memory** (small evolving fact: a user preference, a key project convention) | `memory/` | `memory-core` | The agent should *always* know it. A handful, not hundreds. |
| **Knowledge base** (findings, decisions, reference) | `ref/`, `decisions/` | topical, e.g. `auth` | Looked up on demand via `nt_index` → `nt_search`/`nt_get` — **not** injected, so size is free. |
| **Lesson** (a mistake, footgun, or dead-end not to repeat) | `lessons/` | `lesson` | Surfaced by `nt_recall` at task start — **not** injected, so it costs nothing until relevant. Capture with `nt note … --lesson`. |

So: a durable directive → `nt_note "…" --folder rules --tag rule`; a learned
preference → `nt_note "…" --folder memory --tag memory-core`; a recorded mistake →
`nt_note "…" --lesson` (trigger in the description: "when X, do Y — not Z");
everything else →
a normally-foldered note found later by `nt_search`.

Use stable nt ids and `[[slug]]` / `[[id]]` links to cross-reference; backlinks
resolve automatically. Set `source: opencode` on what you create so it's
distinguishable from the user's hand-entered items (the MCP tools default it).

## Curate

- `nt_mv` — refile/rename a note (rewrites every `[[link]]`).
- `nt_tag` — add/remove tags (e.g. promote a `ref` note into `rule` once it's stable).
- `nt_archive` — retire a stale note from the index/search (reversible).
- `nt_supersede` (handle, by) — mark a note replaced by another; the old one leaves
  the index so a resume sees only the current decision.
- `nt_relink` (from, to) — repoint every `[[link]]` from one handle to another
  (e.g. after superseding, redirect references to the canonical note).

**Dedup guard:** `nt_note` refuses a near-duplicate of an existing note (parallel
agents often record the same decision). When it errors, prefer to **update** the
existing note or **supersede** it; pass `force: true` only for a deliberately
separate note. Watch the `danglingLinks` field in the result — a `[[link]]` that
didn't resolve is a typo to fix.

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
