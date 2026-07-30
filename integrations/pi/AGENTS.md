<!--
  Global rules for Pi, backed by nt.

  This file is intentionally tiny. The substance of your rules and memory lives in
  nt and is injected automatically by the nt-memory extension (compiled live from
  notes tagged `rule` and `memory-core`). Keep durable rules in nt — not inline
  here — so they're editable, searchable, linkable, and versioned in one place.

  Pi concatenates AGENTS.md (or CLAUDE.md) from ~/.pi/agent/, parent dirs, and the
  cwd. Place this at ~/.pi/agent/AGENTS.md (global) or ./AGENTS.md (per project).
-->

# Working agreement

You have a durable memory backend, **nt** (tasks + markdown notes that persist
across sessions). A small, always-current set of **rules** + **core memory** is
already in your context (the `<nt-memory>` block). The larger **knowledge base**
is *not* injected — reach for it on demand. nt's operations are `nt_*` tools (the
nt-memory extension bridges nt's MCP server in as native Pi tools).

## Start of session
- `nt_index` for the KB catalog (note stubs + active tasks, no bodies) and
  `nt_status` for the task feed. Prefer `nt_index format:"compact"` (far fewer
  tokens; large stores tier by default — expand with `folder` or `all:true`).
  Don't re-derive what a past session recorded, and don't bulk-load bodies.

## Before each task — learn from past sessions
- `nt_recall` with a plain-words description of what you're about to do (e.g.
  `context:"adding a cache layer to the API"`). It returns the most relevant
  notes — **lessons/gotchas first** — even when your wording differs. Any
  `lesson:true` result is a past mistake: `nt_get` it and heed it *before*
  writing code.

## As you work
- Capture the *why*: decisions, constraints, dead-ends → `nt_note` (always set a
  one-line `description`); follow-ups → `nt_add` (link via `discovered_from`).
- Pass `if_exists:"return"` on topic notes — a `matched:true` response means the
  canonical note exists: `nt_note_edit` it (with `expect_mtime`) instead of
  creating a sibling; if the edit reverses a conclusion, `nt_decide` the why.
- Volatile facts get a `half_life` (e.g. `"90d"`) so they fade instead of going
  stale; `nt_touch` re-confirms one you verified still holds. A recall result
  with `escalate` means the hit is weak — run the suggested
  `nt_search include_archived:true` before concluding nothing is recorded.
- Set `source:"pi"` on what you create.
- Hit a mistake/footgun/dead-end? Record a **lesson**: `nt_note kind:"lesson"`
  (CLI `nt note … --lesson`), with the *trigger* in the description ("when X, do
  Y — not Z") so `nt_recall` surfaces it next time. **Always give it a `project`
  or a topical `tag`** — `lesson` alone is a structural tag, stripped before the
  duplicate check, so a lesson without one silently bypasses the guard and lets
  you re-record what the store already knows.
- Look something up: `nt_search` for stubs, then `nt_get` the one note you need
  (by id, or a `section`). Fetch on demand — don't preload.
- Use the **`nt` skill** (`/skill:nt`) for the full workflow and folder/tag
  conventions (`rules/`+`rule`, `memory/`+`memory-core`, else = on-demand KB).

## Session review
- `/learn` reviews the session and saves approved learnings; `/recall` loads a
  task-priming or resume brief. If a session did real work but captured nothing,
  the extension nudges you to run `/learn` before it ends.
