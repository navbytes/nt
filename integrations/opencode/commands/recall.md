---
description: Load relevant nt memory on demand — a task-priming brief for a topic, or a where-was-I resume brief when run bare
---

Load the relevant slice of nt — the durable memory store — into this session as
a **compact briefing**. Topic: "$ARGUMENTS".

Stay stub-first: the failure mode to avoid is a context dump. Open full bodies
ONLY where this procedure says to; everything else stays as one-line stubs the
user (or you, later) can `nt_get` on demand.

## If a topic was given — task-priming brief

1. `nt_recall` with the topic as plain words. **Lessons first**: for every
   result with `lesson:true`, `nt_get` the full body — lessons are short and
   they are the payload. Heed them in the work that follows.
2. `nt_search` the topic's key terms (and obvious synonyms) for related
   decisions/reference notes. Keep results as stubs; `nt_get` at most the **2**
   that look genuinely load-bearing for the task.
3. Scan `nt_ready` for open tasks related to the topic.
4. Present the brief, tersely:
   - **Lessons** — each quoted with its trigger ("when X, do Y — not Z").
   - **Relevant notes** — stubs (id · title · one-liner), marking the ones you
     opened; offer to open others on request.
   - **Related open tasks** — id + title.
   Then continue with whatever the user asks next, informed by the brief. If
   recall and search both come back empty, say so in one line — don't pad.

## If no topic was given — resume brief ("where was I?")

If the conversation above already makes the current task obvious, treat that as
the topic and run the task-priming brief instead. Otherwise:

1. `nt_ready` — open, unblocked tasks by urgency (top ~8).
2. `nt_log` for the last few days — what recent sessions completed.
3. `nt_index` — note stubs only; surface the handful most recently updated.
4. Present a short "where things stand" brief: open tasks, recent completions,
   recently-touched notes (stubs). End by asking which thread to pick up — and
   once the user picks one, run the task-priming brief for it before starting.

## Rules

- Total briefing budget: aim under ~1–2K tokens. Never open more than the
  lessons + 2 notes without being asked.
- Use stable ids in the brief so anything can be fetched or updated later.
- If the `nt_*` MCP tools are unavailable, fall back to the `nt` CLI over bash
  (`nt recall … --json`, `nt ready --json`, `nt search … --json`, `nt show <id>`);
  if that's missing too, tell the user to run `nt mcp install --client opencode`
  and stop.
