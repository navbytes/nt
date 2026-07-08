---
description: Review this session for learnings and save the approved ones to nt (lessons, rules, memory, notes, tasks)
---

Review this session and harvest what should outlive it into nt — the durable
memory store. Optional focus: "$ARGUMENTS" (if non-empty, only propose items
related to it).

## 1. Extract candidates

Go back over the whole conversation (everything above this message) and collect
candidate learnings in these buckets:

- **lesson** — a mistake, footgun, or dead-end actually hit this session
  (a failed approach, a wrong assumption, a command that broke something).
- **rule** — a stable directive the user stated or corrected you toward
  ("always X", "never Y", style/process preferences).
- **memory-core** — a small durable fact about the user or project
  (preferences, conventions, environment quirks) that every future session
  should know.
- **note** — a decision, finding, or constraint worth reading back later
  (why an approach was chosen, how a subsystem actually works).
- **task** — concrete unfinished follow-up work discovered but not done.

Then **skip** anything re-discoverable by rereading the code/docs, one-off state,
or raw data/log dumps — memory is for what you *couldn't* re-derive, not a
transcript.

Be **stingy** with `rule` and `memory-core` — those are injected into every
future session and cost tokens forever; only propose them when the session
gives clear evidence they're durable and always-relevant. Be **generous** with
lessons and notes — they cost nothing until retrieved. If the session produced
nothing worth saving, say exactly that and stop; do not invent learnings.

## 2. Dedup before proposing

For each candidate, check it isn't already recorded: `nt_recall` with the
candidate's gist (and `nt_search` for its key terms). Drop duplicates. If an
existing note covers the same ground but is now outdated, propose an **update**
or **supersede** of that note (by id) instead of a new one.

## 3. Present for approval — do not write yet

Show a numbered list. For each item give:

- the bucket, proposed **title**, and one-line **description**
  (for lessons, trigger-form: "when X, do Y — not Z")
- target folder + tag (`lessons/`+`lesson`, `rules/`+`rule`,
  `memory/`+`memory-core`, `decisions/` or `ref/` for notes)
- for `rule` / `memory-core` items, an explicit marker like
  ⚠ *always-in-context — costs tokens on every future request*

Then ask the user which to save (all, numbers like "1 3 4", edits, or none) and
**wait for their reply**. Save nothing without approval.

## 4. Write the approved items

- Notes/rules/memory/lessons → `nt_note` (title, `description`, `body` with the
  detail; `kind:"lesson"|"rule"|"decision"|"ref"|"memory"` applies the canonical
  tag+folder). Set `source:"opencode"`. Apply any edits the user gave.
- Tasks → `nt_add` (verb-first title ≤ ~60 chars, detail in `body`, link related
  work with `discovered_from`).
- `nt_note` always creates; if its response includes a `similar` list, check
  whether you just doubled an existing note — if so, consolidate: extend the
  original (`nt_note_edit` with `append` or `old_string`+`new_string`; CLI
  `nt edit --append`) or retire the duplicate with `nt_archive`
  `superseded_by=<kept id>`. (The CLI fallback `nt note` does refuse
  near-duplicates — follow its printed guidance.)
- Finish with a short receipt: each saved item's id and where it went.

If the `nt_*` MCP tools are unavailable, fall back to the `nt` CLI over bash
(`nt note …`, `nt add …`, `nt recall …`; multi-line or backtick-laden bodies:
`--body-file -` with the body piped in); if that's missing too, tell the user
to run `nt mcp install --client opencode` and stop.
