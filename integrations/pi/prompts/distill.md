---
description: Consolidate the nt store — merge near-duplicate notes, then prune the always-loaded rules/memory block (human-gated, never silent)
argument-hint: "[focus|rules]"
---

Two hygiene passes over the nt store, both human-gated. Pass 1 fixes the store
rot that degrades **recall**; pass 2 fixes the rot you pay **tokens** for on
every single request.

Optional "$@": a focus term (only review items touching it), or the literal
`rules` to skip pass 1 and run pass 2 alone.

Run pass 1 first even when the store looks clean — it's one read-only call.
A clean result means "go to pass 2", **not** "stop".

---

# Pass 1 — near-duplicate notes

## 1.1 List the candidates

Call `nt_distill` (read-only — it never merges anything). Empty result means the
note corpus is clean; say so and go straight to pass 2.

## 1.2 Review each pair

For each pair, `nt_get` both notes (don't guess from the stub). Decide:

- **True duplicate** — same fact, one is stale or less complete. Propose
  merging the better one into the other.
- **Deliberate fork** — same title shape but genuinely distinct content
  (e.g. per-project variants). Propose tagging one `distinct` and moving on;
  never merge these.
- **Unclear** — say so and ask, don't guess.

## 1.3 Hold the proposals

Don't write yet — pass 2's proposals join the same approval list (§3).

---

# Pass 2 — the always-loaded block

Notes in `rules/` (tag `rule`) and `memory/` (tag `memory-core`) are injected
into or compiled into **every** session. Unlike lessons and notes — free until
retrieved — these are paid for on every request, forever. This pass is where
consolidation actually buys something.

## 2.1 Load what's actually paid for

`nt_index` with `tag:"rule"`, then again with `tag:"memory-core"`, then `nt_get`
each body — they're small by design, and if there are so many that this feels
expensive, *that is itself the finding*. Report the total character count before
proposing anything. (CLI: `nt export --tag rule --format md` prints byte-for-byte
what compiles into `AGENTS.md`/`CLAUDE.md`.)

## 2.2 Look for four things

- **Subsumption** — a general rule already covers a specific one. Keep the
  general, retire the specific.
- **Contradiction** — two rules that point opposite ways. Highest severity:
  nothing resolves it at read time, so the agent silently picks one and neither
  you nor the user knows which. Never auto-resolve — surface it and ask.
- **Dead trigger** — the rule's precondition no longer exists (it names a file,
  command, or dependency that's gone). **Verify before proposing**: grep the
  repo for what it names. Don't retire a rule on a guess.
- **Not actually always-relevant** — it only fires in one narrow context. The
  most common finding and the most valuable one, because the fix is a
  **demotion, not a deletion**: a rule demoted to a lesson still fires through
  `nt_recall` when its situation comes up — it just stops being billed on every
  unrelated request.

Be conservative in the other direction too: a short, genuinely always-on rule
earns its tokens. Don't propose pruning something just to have a finding.

---

# 3. Present everything for approval — do not write yet

One numbered list, both passes together. Per item: which pass, what you'd do,
and a one-line reason.

- Pass 1 items: which pair, which one you'd keep, a one-line summary of what
  the merged body would say, and what happens to the other (retired via
  `superseded_by`, or tagged `distinct`).
- Pass 2 items: which of the four findings, the proposed action, and for a
  demotion, where it lands. Flag every pass-2 item ⚠ — *changes what every
  future session sees*.
- Close with the block's current character count and what it would be if all
  pass-2 items are approved.

Then ask which to apply (all, numbers, edits, or none) and **wait**. Nothing is
written without approval.

# 4. Apply the approved items

**Merges (pass 1, and subsumed/contradicting rules from pass 2):**

- Fold content into the kept note: `nt_note_edit` (`append`, or
  `old_string`+`new_string` for a targeted fix) — build the full merged text
  yourself, don't just concatenate.
- Retire the other one: `nt_archive` with `handle` set to the retired note
  and `superseded_by` set to the kept note's id (reversible — the file stays
  on disk, `nt_archive undo:true` brings it back).
- Deliberate fork instead: `nt_tag` `add:["distinct"]` on one of the pair —
  future `nt_distill`/`nt doctor` runs stop flagging it.

**Demotions (pass 2):**

- `nt_mv` the note to `lessons/` (or `ref/` if it's reference material rather
  than a hard-won mistake), then `nt_tag` `remove:["rule"]` (or
  `["memory-core"]`) `add:["lesson"]`. The body stays; only its class changes.
- Lessons rank best in trigger-form — if the description isn't already
  "when X, do Y — not Z", rewrite it with `nt_note_edit` `description`.

**Dead triggers (pass 2):** `nt_archive` — no `superseded_by`, nothing replaced
it. Reversible via `nt_archive undo:true`.

Finish with a short receipt: what was merged, demoted, retired, tagged
`distinct`, and skipped — plus the block's before/after size. If anything in
`rules/`/`memory/` changed and the user compiles them into `AGENTS.md` /
`CLAUDE.md`, remind them to re-run `nt export --tag rule` — otherwise the
pruning never reaches the place it was being paid for.

If the `nt_*` tools are unavailable, fall back to the `nt` CLI over bash (`nt
distill`, `nt show`, `nt index --tag rule`, `nt export --tag rule`, `nt edit
--append`, `nt supersede <old> --by <new>`, `nt mv <note> lessons/<name>`, `nt
tag <id> +lesson -rule`, `nt archive <id>`); if that's missing too, tell the
user to run `nt pi install` and stop.
