<!--
  Global rules for OpenCode, backed by nt.

  This file is intentionally tiny. The substance of your rules and memory lives in
  nt and is injected into context automatically by the nt-memory plugin (compiled
  live from notes tagged `rule` and `memory-core`). Keep durable rules in nt — not
  inline here — so they're editable, searchable, linkable, and versioned in one place.

  Place this at ~/.config/opencode/AGENTS.md (global) or ./AGENTS.md (per project).
-->

# Working agreement

You have a durable memory backend called **nt** (tasks + markdown notes that
persist across sessions). A small, always-current set of **rules** and **core
memory** from nt is already injected into your context (look for the
`<nt-memory>` block). The larger **knowledge base** is *not* injected — reach for
it on demand.

## Start of session
- Call **`nt_index`** for the KB catalog (note stubs + active tasks — no bodies)
  and **`nt_ready`** for the task feed before starting substantive work. Don't
  re-derive what a past session recorded, and don't bulk-load note bodies.

## As you work
- Capture the *why*: record decisions, constraints, and dead-ends with **`nt_note`**
  (always set a one-line `description`); capture follow-ups with **`nt_add`**
  (link discovered work via `discovered_from`).
- To look something up: **`nt_search`** for ranked stubs, then **`nt_get`** the one
  note you need (by id, or a single `section`). Fetch on demand — don't preload.
- Use the **`nt` skill** for the full workflow and the folder/tag conventions
  (`rules/`+`rule`, `memory/`+`memory-core`, everything else = on-demand KB).

## Lazy-loading file references
- OpenCode does **not** auto-expand `@path/to/file.md` references. When you see one
  in context, load it yourself with the Read tool only if it's relevant to the task.
