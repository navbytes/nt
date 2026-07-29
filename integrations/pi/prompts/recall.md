---
description: Load relevant nt memory on demand — a task-priming brief for a topic, or a where-was-I resume brief when run bare
argument-hint: "[topic]"
---

Load the relevant slice of nt — the durable memory store — as a **compact
briefing**. Topic: "$@".

Stay stub-first: the failure mode to avoid is a context dump. Open full bodies
ONLY where told; everything else stays as one-line stubs, `nt_get`-able on demand.

## If a topic was given — task-priming brief

1. `nt_recall` with the topic as plain words. **Lessons first**: for every
   `lesson:true` result, `nt_get` the full body (lessons are short and they're
   the payload) and heed it. Check each result's **confidence tier**
   (`strong`/`medium`/`weak`) — the tier matters, not the raw score (which is
   query-dependent).
2. `nt_search` the topic's key terms (and obvious synonyms) for related
   decisions/reference notes. Keep them as stubs; `nt_get` at most the **2** that
   look genuinely load-bearing.
3. Scan `nt_status` for related open tasks.
4. Present the brief, tersely: **Lessons** (each quoted with its trigger),
   **Relevant notes** (stubs, marking the ones you opened), **Related open tasks**
   (id + title). Then continue with whatever the user asks next. If recall and
   search both come back empty, say so in one line — that's signal, not an error.

## If no topic was given — resume brief ("where was I?")

If the conversation already makes the current task obvious, treat that as the
topic and run the task-priming brief. Otherwise:

1. `nt_status` — in-progress/blocked/open tasks by urgency + recent completions.
2. `nt_index` — note stubs; surface the handful most recently updated.
3. Present a short "where things stand" brief (open tasks, recent completions,
   recently-touched note stubs) and ask which thread to pick up — then run the
   task-priming brief for it before starting.

## Rules

- Budget: aim under ~1–2K tokens. Never open more than the lessons + 2 notes
  without being asked.
- Use stable ids so anything can be fetched or updated later.
- If the `nt_*` tools are unavailable, fall back to the `nt` CLI over bash (`nt
  recall … --json`, `nt ready --json`, `nt search … --json`, `nt show <id>`); if
  that's missing too, tell the user to run `nt pi install` and stop.
