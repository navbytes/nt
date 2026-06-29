---
name: nt
description: Capture and recall durable memory in the user's nt store across OpenCode sessions. Use when asked to save/track a task or TODO, take a note, record a decision or finding, mark something done, search or recall what was captured before, or organize the knowledge base. nt persists everything as plain files (tasks + markdown notes) that outlive the session.
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

## The loop

**At the start of substantive work**, reload context before creating anything:

- `nt_ready` — open, unblocked tasks by urgency. Your "pick up here" feed.
- `nt_recall` — the broader read: prior tasks **and note bodies** you captured before.
- `nt_search` — look up a specific topic before acting, to avoid duplicating a note.

**As you work**, capture the *why*, not just the *what*:

- `nt_add` — a task. Short, verb-first title (~60 chars); put detail in `body`.
  Chain discovered work with `discovered_from: <id>`.
- `nt_note` — a finding, decision, constraint, or dead-end. The body is what a
  future session reads back, so write it like a comment for the next engineer.
- `nt_done` / `nt_update` — complete or change a task by its **stable id**
  (never a row number).
- `nt_links` — follow forward links + backlinks to reconstruct why something exists.

## Where things go (folders + tags)

This setup reserves two folders/tags for the **always-in-context** layer the
plugin injects into every session — keep them small and high-signal:

| Put it here | Folder | Tag | When |
|-------------|--------|-----|------|
| **Rule** (stable directive: "always run gofmt", style/process) | `rules/` | `rule` | Must apply every session. Costs tokens every turn — keep terse. |
| **Core memory** (small evolving fact: a user preference, a key project convention) | `memory/` | `memory-core` | The agent should *always* know it. A handful, not hundreds. |
| **Knowledge base** (findings, decisions, reference) | `ref/`, `decisions/` | topical, e.g. `auth` | Looked up on demand via `nt_search` — **not** injected, so size is free. |

So: a durable directive → `nt_note "…" --folder rules --tag rule`; a learned
preference → `nt_note "…" --folder memory --tag memory-core`; everything else →
a normally-foldered note found later by `nt_search`.

Use stable nt ids and `[[slug]]` / `[[id]]` links to cross-reference; backlinks
resolve automatically. Set `source: opencode` on what you create so it's
distinguishable from the user's hand-entered items (the MCP tools default it).

## Curate

- `nt_mv` — refile/rename a note (rewrites every `[[link]]`).
- `nt_tag` — add/remove tags (e.g. promote a `ref` note into `rule` once it's stable).
- `nt_archive` — retire a stale note from recall/search (reversible).

## Conventions

- **Retrieve before you create** (`nt_recall` / `nt_search`) to avoid duplicates.
- Tasks are one line; anything longer is a note `body`, linked from the task.
- Keep the `rules/` + `memory/` core **small** — it's billed on every request.
  The big knowledge base belongs behind `nt_search`, where it's free until used.
- Promote, don't duplicate: when a `ref` note becomes a standing rule, retag it
  rather than copying it into the rules core.
