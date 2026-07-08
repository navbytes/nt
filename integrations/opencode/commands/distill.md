---
description: Review near-duplicate notes and merge the approved ones (human-gated, never silent)
---

Consolidate near-duplicate notes in nt — the store rot that degrades recall
most. Optional focus: "$ARGUMENTS" (if non-empty, only review pairs touching
it).

## 1. List the candidates

Call `nt_distill` (read-only — it never merges anything). Empty result means
the store is clean; say so and stop.

## 2. Review each pair

For each pair, `nt_get` both notes (don't guess from the stub). Decide:

- **True duplicate** — same fact, one is stale or less complete. Propose
  merging the better one into the other.
- **Deliberate fork** — same title shape but genuinely distinct content
  (e.g. per-project variants). Propose tagging one `distinct` and moving on;
  never merge these.
- **Unclear** — say so and ask, don't guess.

## 3. Present for approval — do not write yet

Show a numbered list: which pair, which one you'd keep, a one-line summary of
what the merged body would say, and what happens to the other (retired via
`superseded_by`, or tagged `distinct`). Then ask which to apply (all, numbers,
edits, or none) and **wait**. Merge nothing without approval.

## 4. Apply the approved merges

- Fold content into the kept note: `nt_note_edit` (`append`, or
  `old_string`+`new_string` for a targeted fix) — build the full merged text
  yourself, don't just concatenate.
- Retire the other one: `nt_archive` with `handle` set to the retired note
  and `superseded_by` set to the kept note's id (reversible — the file stays
  on disk, `nt_archive undo:true` brings it back).
- Deliberate fork instead: `nt_tag` `add:["distinct"]` on one of the pair —
  future `nt_distill`/`nt doctor` runs stop flagging it.
- Finish with a short receipt: what was merged, what was tagged distinct,
  what was skipped.

If the `nt_*` MCP tools are unavailable, fall back to the `nt` CLI over bash
(`nt distill`, `nt show`, `nt edit --append`, `nt supersede <old> --by <new>`,
`nt tag <id> +distinct`); if that's missing too, tell the user to run `nt mcp
install --client opencode` and stop.
