---
description: Review this session for learnings and save the approved ones to nt (lessons, rules, memory, notes, tasks)
argument-hint: "[focus]"
---

Review this session and harvest what should outlive it into nt — the durable
memory store. Optional focus: "$@" (if non-empty, only propose items related to
it).

## 1. Extract candidates

Go back over the whole conversation and collect candidates in these buckets:

- **lesson** — a mistake/footgun/dead-end actually hit (a failed approach, a
  wrong assumption, a command that broke something).
- **rule** — a stable directive the user stated or corrected you toward
  ("always X", "never Y", style/process).
- **memory-core** — a small durable fact about the user or project (preferences,
  conventions, environment quirks) every future session should know.
- **note** — a decision, finding, or constraint worth reading back later.
- **task** — concrete unfinished follow-up work.

Then **skip** anything re-discoverable by rereading the code/docs, one-off state,
or raw data/log dumps — memory is for what you *couldn't* re-derive, not a
transcript.

Be **stingy** with `rule` and `memory-core` — they're injected into every future
session and cost tokens forever; propose them only on clear evidence they're
durable and always-relevant. Be **generous** with lessons and notes — free until
retrieved. If the session produced nothing worth saving, say so and stop; don't
invent learnings.

## 2. Dedup before proposing

For each candidate, check it isn't already recorded (`nt_recall` the gist,
`nt_search` its key terms). Drop duplicates. If an existing note covers the same
ground but is outdated, propose an **update**/**supersede** of it (by id) instead.

## 3. Present for approval — do not write yet

Show a numbered list. Per item: bucket, proposed **title**, one-line
**description** (lessons in trigger-form "when X, do Y — not Z"), and target
folder+tag (`lessons/`+`lesson`, `rules/`+`rule`, `memory/`+`memory-core`,
`decisions/`/`ref/` for notes). Flag `rule`/`memory-core` items ⚠
*always-in-context — costs tokens on every future request*. Then ask which to
save (all, numbers, edits, or none) and **wait**. Save nothing without approval.

## 4. Write the approved items

- Notes/rules/memory/lessons → `nt_note` (title, `description`, `body`;
  `kind:"lesson"|"rule"|"decision"|"ref"|"memory"` applies the canonical
  tag+folder). Set `source:"pi"`. Apply the user's edits.
- Tasks → `nt_add` (verb-first title ≤ ~60 chars, detail in `body`, link via
  `discovered_from`).
- If `nt_note`'s response has a `similar` list, check whether you doubled a note;
  if so, consolidate — extend the original (`nt_note_edit` append or
  `old_string`+`new_string`; CLI `nt edit --append`) or retire the duplicate
  (`nt_archive superseded_by=<kept id>`).
- Finish with a short receipt: each saved item's id and where it went.

If the `nt_*` tools are unavailable, fall back to the `nt` CLI over bash (`nt
note …`, `nt add …`, `nt recall …`; multi-line/backtick bodies via `--body-file
-`); if that's missing too, tell the user to run `nt pi install` and stop.
