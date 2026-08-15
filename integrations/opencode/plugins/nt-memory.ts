/**
 * nt-memory — wire nt into OpenCode as the rules + memory backend.
 *
 * What it does
 * ------------
 * 1. Injects an always-in-context block (your nt **rules** + a small **core
 *    memory**) into the system prompt at assembly time, recompiled live from nt
 *    on every session — so editing a note in nt updates what the agent sees, with
 *    no stale exported file. Uses the `experimental.chat.system.transform` hook,
 *    which fires per model request (every tool round-trip, not once per turn);
 *    the compiled block is memoized (keyed by `nt store-hash`) so that doesn't
 *    mean re-execing nt or re-emitting different bytes on every request — see
 *    the compile()/compileUncached() split below.
 * 2. **Compaction survival** (on by default) — when OpenCode compacts a long
 *    session, pushes the open nt tasks and a "re-recall after compaction" directive
 *    into the compaction context, so in-flight work and the memory workflow survive
 *    summarization. Uses the `experimental.session.compacting` hook.
 * 3. **Error-triggered recall** (on by default) — when a bash tool call fails,
 *    runs `nt recall --lessons-only` on the command + error and injects any
 *    recorded lessons into the next model request. Turns lessons from pull (the
 *    agent must remember to ask) into push (the mistake summons its own antidote).
 * 4. **Idle capture nudge** (on by default) — if a session did real work but
 *    captured nothing into nt, shows a one-time TUI toast suggesting a note, so
 *    sessions don't end with zero recorded learning.
 * 5. (Optional, off by default) keeps a static markdown file fresh so the
 *    documented `instructions` config path works on clients/versions without the
 *    experimental hook. Toggle with NT_INJECT=file.
 * 6. (Optional, off by default) mirrors OpenCode's todo list into nt tasks on
 *    `todo.updated` — the OpenCode analog of Claude Code's `nt hook`.
 *
 * The knowledge base (everything in nt outside the small rules/memory core) is
 * NOT injected — it stays behind the nt MCP tools (nt_index / nt_search /
 * nt_recall / nt_get / nt_links), fetched on demand so it costs zero tokens
 * until used.
 * Register those once with:  nt mcp install --client opencode
 *
 * Everything here is wrapped so a missing or broken nt can never break a session.
 *
 * nt conventions this relies on (see the bundle README):
 *   - folder `rules/`  + tag `rule`        → always-in-context rules
 *   - folder `memory/` + tag `memory-core` → small evolving core memory
 *   - tag `lesson`                          → recorded mistakes, ranked first by recall
 *   - everything else                       → on-demand KB via the MCP tools
 */
import type { Plugin } from "@opencode-ai/plugin"
import fs from "node:fs/promises"
import os from "os"
import path from "path"

const HOME = os.homedir()

const CONFIG = {
  // nt binary. Override with NT_BIN if it isn't on OpenCode's PATH (GUI launches
  // often miss ~/.local/bin).
  ntBin: process.env.NT_BIN || "nt",

  // "hybrid" (default) → write a session-start file baseline (guaranteed —
  //                      loaded via the STABLE `instructions` config) AND push
  //                      live updates via the experimental system-prompt
  //                      transform whenever the store changes after that
  //                      snapshot. This is the fix for a real failure mode:
  //                      experimental.chat.system.transform is reported to
  //                      silently no-op on some OpenCode builds (the runtime
  //                      discards the mutation before the LLM sees it —
  //                      sst/opencode#17100, closed "not planned"), and the
  //                      old "system" default had NO fallback — a default
  //                      install on an affected build injected zero rules,
  //                      silently. The file baseline can't silently no-op the
  //                      same way; the transform layers freshness on top when
  //                      it works, deduped against the snapshot so a working
  //                      hook doesn't double-inject unchanged content.
  // "system"           → transform-only, no file baseline (the old default) —
  //                      keep for anyone who confirmed their build's
  //                      experimental.chat.system.transform actually reaches
  //                      the model and prefers not to touch opencode.json.
  // "file"             → file-only, no live transform push.
  // "off"              → don't inject; rely on AGENTS.md + on-demand MCP only.
  injectMode: (process.env.NT_INJECT || "hybrid") as "hybrid" | "system" | "file" | "off",

  rulesTag: "rule",
  memoryTag: "memory-core",

  // file-mode / hybrid-mode target; must match the path in opencode.json
  // `instructions`. Holds the SAME compiled rules+memory block compile()
  // produces (truncated to NT_INJECT_MAX, no-provenance) — not a second, raw
  // `nt export` call with different flags, which would drift from what the
  // live path shows and could bypass the injection budget entirely.
  instructionsFile:
    process.env.NT_RULES_FILE || path.join(HOME, ".config", "opencode", "nt-rules.md"),

  // Hard cap on injected characters so the always-in-context block can't blow the
  // token budget. Keep rules/memory notes short; overflow is truncated with a note.
  maxInjectChars: Number(process.env.NT_INJECT_MAX || 8000),

  // Push open nt tasks + a re-recall directive into the compaction context so
  // in-flight work survives summarization. Disable with NT_COMPACT=0.
  compactContext: process.env.NT_COMPACT !== "0",

  // On a failed bash call, `nt recall --lessons-only` the command + error and
  // inject matching lessons into the next request. Disable with NT_ERROR_RECALL=0.
  errorRecall: process.env.NT_ERROR_RECALL !== "0",

  // One-time toast when a session that used tools ends idle without a single nt
  // write. Disable with NT_IDLE_NUDGE=0.
  idleNudge: process.env.NT_IDLE_NUDGE !== "0",

  // Mirror OpenCode todos → nt tasks. Off by default: the agent already captures
  // tasks via the nt_add MCP tool, and a one-way mirror without dedup can double
  // up. Enable with NT_MIRROR_TODOS=1 if you prefer passive capture.
  mirrorTodos: process.env.NT_MIRROR_TODOS === "1",
}

// MCP write tools, matched by suffix so both `nt_note` and a server-prefixed
// `nt_nt_note` count (OpenCode names MCP tools `<server>_<tool>`).
// Keep in sync with the Pi extension. `note_edit` was missing here: the regex
// was written for the idle nudge before nt_note_edit existed, then reused
// verbatim when it gained a second job (cache invalidation) without being
// re-derived from the tool list. Being $-anchored, neither `nt_note_edit`
// nor OpenCode's `nt_nt_note_edit` matched — so an edit to a rules/ or
// memory/ note stayed invisible to the injected block for up to
// NT_INJECT_TTL, and a session that captured only via nt_note_edit still
// got told it had saved nothing. /learn and /distill drive most of their
// writes through exactly that tool.
const NT_WRITE_TOOL = /(^|_)nt_(add|note|note_edit|update|tag|mv|rm|archive|relink)$/

export const NtMemory: Plugin = async ({ $, client }) => {
  // Run an nt subcommand, returning stdout (empty string on any failure). A
  // failure is otherwise completely silent — every caller here treats "" as
  // "no data" and degrades gracefully — so if nt can't be run AT ALL (not on
  // PATH, GUI launches often miss ~/.local/bin) the whole integration goes
  // quiet with zero signal. Warn ONCE so that's diagnosable instead of a
  // mystery "why isn't my rule showing up" report.
  let warnedMissingNt = false
  const nt = async (args: string[]): Promise<string> => {
    try {
      return await $`${CONFIG.ntBin} ${args}`.text()
    } catch (e) {
      if (!warnedMissingNt) {
        warnedMissingNt = true
        console.warn(
          `[nt-memory] \`${CONFIG.ntBin} ${args.join(" ")}\` failed (${(e as any)?.message || e}). ` +
            `nt-memory degrades silently from here on failure by design — if rules/memory never show up, ` +
            `nt is likely not resolvable from OpenCode's process; set NT_BIN to its absolute path.`,
        )
      }
      return ""
    }
  }

  const ntJSON = async (args: string[]): Promise<any> => {
    const out = await nt(args)
    if (!out.trim()) return null
    try {
      return JSON.parse(out)
    } catch {
      return null
    }
  }

  // ---- per-session state (bounded; a long-lived server must not leak) ----
  const capped = <T>(set: Set<T>, cap = 256) => {
    if (set.size > cap) set.clear()
    return set
  }
  const sessionsWithToolUse = new Set<string>()
  const sessionsWithNtWrites = new Set<string>()
  const sessionsNudged = new Set<string>()
  const recalledFailures = new Set<string>() // throttle: one recall per distinct failing command
  // Lessons recalled from a failure, waiting to ride the next system-prompt build.
  // Injected once, then cleared — a one-turn nudge, not a standing token cost.
  const pendingLessons = new Map<string, string>()
  // Hybrid mode: the compiled text written to the instructions files at
  // session.created. The transform hook only pushes live content when it
  // differs from this — otherwise the model already has it via `instructions`
  // and pushing again would show every rule twice. null means no snapshot
  // exists yet (e.g. session.created never fired) — treated as "always push"
  // so a missing baseline fails toward showing rules, not hiding them.
  let fileSnapshotText: string | null = null

  // Compile the always-in-context block: rules first, then core memory. Each is a
  // separate `nt export` (tag filters are AND-combined, so two tags need two
  // calls). Provenance AND the top "Generated by" header are dropped to save
  // tokens on every turn — the agent can still nt_search/nt_get the source note.
  const compileUncached = async (): Promise<string> => {
    const [rules, memory] = await Promise.all([
      nt(["export", "--tag", CONFIG.rulesTag, "--title", "Rules", "--no-provenance", "--no-header"]),
      nt(["export", "--tag", CONFIG.memoryTag, "--title", "Memory", "--no-provenance", "--no-header"]),
    ])
    const out = [rules.trim(), memory.trim()].filter(Boolean).join("\n\n")
    const max = CONFIG.maxInjectChars
    if (out.length <= max) return out
    // Over budget. NEVER slice mid-note (that silently truncates a rule's body and
    // drops the next rule with no signal — the agent then violates a rule it was
    // never shown). Split at heading boundaries and greedily keep every WHOLE note
    // that fits; if not even the first rule fits, keep it explicitly truncated
    // (never an empty block); report how many notes were omitted.
    const blocks = out.split(/\n(?=#{1,2} )/) // each block = a heading + its body
    const isNote = (b: string) => /^## /.test(b)
    const kept: string[] = []
    let used = 0
    let omitted = 0
    let truncatedOne = false
    for (const b of blocks) {
      const add = (used > 0 ? 1 : 0) + b.length // +1 for the "\n" join
      if (used + add <= max) {
        kept.push(b)
        used += add
      } else if (isNote(b) && !kept.some(isNote) && !truncatedOne) {
        // Nothing substantive kept yet and this note alone overflows: keep it
        // truncated so a lone oversized rule is still (partly) shown, not dropped.
        const room = Math.max(0, max - used - 100)
        kept.push(
          b.slice(0, room).trimEnd() +
            "\n⚠ nt: this rule was truncated to fit NT_INJECT_MAX — tell the user to shorten it.",
        )
        used = max
        truncatedOne = true
      } else if (isNote(b)) {
        omitted++
      }
    }
    if (omitted > 0 || truncatedOne) {
      console.warn(
        `[nt-memory] rules+memory (${out.length} chars) exceed NT_INJECT_MAX=${max}; ` +
          `omitted ${omitted} whole note(s)${truncatedOne ? " and truncated 1" : ""}. ` +
          `Trim rules/memory-core notes or raise NT_INJECT_MAX.`,
      )
    }
    let result = kept.join("\n")
    if (omitted > 0) {
      // An agent-visible imperative, not an HTML comment — models routinely ignore
      // or strip comments, and the whole point is to pressure the user to curate.
      result +=
        `\n\n⚠ nt-memory is OVER its ${max}-char budget: ${omitted} rule/memory note(s) are NOT shown, ` +
        `so some rules are currently invisible to you. Tell the user to consolidate or archive ` +
        `rules/ + memory-core notes (or raise NT_INJECT_MAX).`
    }
    return result
  }

  // ---- compile() memoization ----
  // compileUncached() re-execs two `nt export` calls, and OpenCode fires
  // experimental.chat.system.transform per MODEL REQUEST — every tool
  // round-trip within a turn, not once per user turn — so unmemoized this
  // spawns dozens of nt processes per session and re-emits fresh bytes every
  // time, guaranteeing a prompt-cache MISS on the system block every request.
  // The cache is keyed by `nt store-hash` (a cheap stat-walk over notes/, no
  // bodies read) rather than the compiled bytes themselves, so unchanged
  // content returns byte-identical cached output — the actual precision an LLM
  // provider's prompt cache needs. A short TTL bounds how often the hash
  // itself gets checked; write signals (an nt_* write tool, or a bash command
  // that runs `nt note/add/…`) drop the cache immediately so an edit made THIS
  // turn shows up THIS turn, not after the TTL.
  let compileCache: { hash: string; text: string; ts: number } | null = null
  const compileTTLMs = Number(process.env.NT_INJECT_TTL || 15000)
  const invalidateCompileCache = () => {
    compileCache = null
  }
  const compile = async (): Promise<string> => {
    const now = Date.now()
    if (compileCache && now - compileCache.ts < compileTTLMs) {
      return compileCache.text
    }
    const hash = (await nt(["store-hash"])).trim()
    if (hash && compileCache && hash === compileCache.hash) {
      compileCache.ts = now // still fresh — refresh the TTL clock, skip re-export
      return compileCache.text
    }
    const text = await compileUncached()
    compileCache = { hash, text, ts: now }
    return text
  }

  // Write the file baseline for "file"/"hybrid" mode, loaded via opencode.json's
  // `instructions`. Writes compileUncached()'s OWN output — not a second, raw
  // `nt export` call — so the file is byte-identical to what the live path
  // would show (same truncation budget, same no-provenance formatting) rather
  // than independently drifting. Records the text as fileSnapshotText so the
  // transform hook can dedupe against it. Uses compileUncached(), not
  // compile(), so the snapshot is always truly fresh at session start
  // regardless of the turn-scoped memoization cache.
  const writeInstructionsFiles = async (): Promise<void> => {
    const text = await compileUncached()
    fileSnapshotText = text
    try {
      await fs.writeFile(CONFIG.instructionsFile, text || "# Rules\n\n(no rules or memory notes captured yet)\n", "utf8")
    } catch {
      /* best-effort — the live transform path (when it works) still covers this */
    }
  }

  // A bash command that drives nt's write surface directly (fallback path, or
  // an agent that prefers the CLI) — same invalidation trigger as a bridged
  // nt_* write tool.
  const NT_WRITE_COMMAND = /(^|[;&|]\s*)nt\s+(note|add|update|tag|mv|rm|archive|relink|edit)\b/

  // Recall recorded lessons for a failed command; empty string when none match.
  const recallLessons = async (command: string, errorTail: string): Promise<string> => {
    const query = [command.split("\n")[0].slice(0, 120), errorTail.slice(0, 160)]
      .filter(Boolean)
      .join(" ")
      .trim()
    if (!query) return ""
    const stubs = await ntJSON(["recall", query, "--lessons-only", "--json", "--limit", "3"])
    if (!Array.isArray(stubs) || stubs.length === 0) return ""
    const lines = stubs.slice(0, 3).map((s: any) => {
      const desc = (s.description || "").trim()
      return `- ${s.id} ${s.title}${desc ? ` — ${desc}` : ""}`
    })
    return (
      `<nt-lessons trigger="a bash command just failed">\n` +
      `Recorded lessons from past sessions that may explain this failure — check them BEFORE retrying:\n` +
      `${lines.join("\n")}\n` +
      `Fetch details with the nt_get tool.\n</nt-lessons>`
    ).slice(0, 1600)
  }

  // Failure detection across OpenCode versions: exit code lives in tool metadata
  // under one of a few names; a missing/zero code means success.
  const failedExit = (metadata: any): boolean => {
    for (const k of ["exit", "exitCode", "exit_code", "code"]) {
      const v = metadata?.[k]
      if (typeof v === "number") return v !== 0
    }
    return false
  }

  return {
    // Inject live, every session, as the system prompt is built. The rules+memory
    // block is stable within a session (cache-friendly); a pending lessons block
    // (from a failed command) is appended for ONE request, then cleared — that
    // single request pays a prompt-cache miss, which is the cost of the nudge.
    "experimental.chat.system.transform": async (_input: any, output: any) => {
      const wantsLive = CONFIG.injectMode === "system" || CONFIG.injectMode === "hybrid"
      if (!wantsLive && pendingLessons.size === 0) return
      try {
        if (!Array.isArray(output?.system)) return
        if (CONFIG.injectMode === "system") {
          const text = await compile()
          if (text) {
            output.system.push(
              `<nt-memory source="nt store — edit notes in nt, not here">\n${text}\n</nt-memory>`,
            )
          }
        } else if (CONFIG.injectMode === "hybrid") {
          // Only push when the store changed since the session-start file
          // snapshot (or no snapshot exists) — otherwise the model already has
          // this via `instructions` and pushing again would show every rule
          // twice. When this hook is a no-op on the current build (#17100),
          // this branch simply never runs — the file baseline still covers it.
          const text = await compile()
          if (text && text !== fileSnapshotText) {
            output.system.push(
              `<nt-memory source="nt store — updated since session start; edit notes in nt, not here">\n${text}\n</nt-memory>`,
            )
          }
        }
        if (pendingLessons.size > 0) {
          for (const block of pendingLessons.values()) output.system.push(block)
          pendingLessons.clear()
        }
      } catch {
        /* never break a session over memory injection */
      }
    },

    // Compaction survival: give the summarizer the open nt tasks and tell the
    // continuation to lean on nt — otherwise summaries routinely drop both.
    "experimental.session.compacting": async (_input: any, output: any) => {
      if (!CONFIG.compactContext) return
      try {
        if (!Array.isArray(output?.context)) return
        const tasks = await ntJSON(["ready", "--json"])
        const lines = Array.isArray(tasks)
          ? tasks.slice(0, 12).map((t: any) => `- [${t.id}] ${t.text}`)
          : []
        const parts = [
          "<nt-memory-compaction>",
          "This project has a durable nt memory store (MCP tools nt_index/nt_search/nt_recall/nt_get; writes via nt_add/nt_note).",
        ]
        if (lines.length > 0) {
          parts.push("Open nt tasks — preserve these in the summary, they are the in-flight work:", ...lines)
        }
        parts.push(
          "After compaction: call nt_recall with the current task before resuming, and keep capturing decisions/lessons with nt_note.",
          "</nt-memory-compaction>",
        )
        output.context.push(parts.join("\n").slice(0, 2000))
      } catch {
        /* compaction must never fail because of us */
      }
    },

    // Post-tool: (a) track nt writes per session for the idle nudge; (b) on a
    // failed bash call, queue matching lessons for the next request.
    "tool.execute.after": async (input: any, output: any) => {
      try {
        const sid: string = input?.sessionID || ""
        const tool: string = input?.tool || ""
        const command: string = tool === "bash" ? String(input?.args?.command ?? "") : ""
        if (sid) {
          capped(sessionsWithToolUse).add(sid)
          if (NT_WRITE_TOOL.test(tool)) capped(sessionsWithNtWrites).add(sid)
        }
        if (NT_WRITE_TOOL.test(tool) || (tool === "bash" && NT_WRITE_COMMAND.test(command))) {
          invalidateCompileCache()
        }

        if (!CONFIG.errorRecall || tool !== "bash") return
        if (!failedExit(output?.metadata)) return
        const throttleKey = command.slice(0, 80)
        if (!throttleKey || recalledFailures.has(throttleKey)) return
        capped(recalledFailures, 128).add(throttleKey)
        const text = typeof output?.output === "string" ? output.output : ""
        const errorTail =
          text
            .trim()
            .split("\n")
            .filter((l: string) => l.trim())
            .slice(-2)
            .join(" ") || ""
        const block = await recallLessons(command, errorTail)
        if (block) {
          if (pendingLessons.size > 8) pendingLessons.clear()
          pendingLessons.set(sid || throttleKey, block)
        }
      } catch {
        /* passive hooks must never throw */
      }
    },

    event: async ({ event }: { event: { type: string; properties?: any } }) => {
      try {
        // File/hybrid mode: refresh the instructions-backed file at session
        // start — the guaranteed layer hybrid mode adds on top of the
        // (possibly no-op) live transform.
        if ((CONFIG.injectMode === "file" || CONFIG.injectMode === "hybrid") && event.type === "session.created") {
          await writeInstructionsFiles()
        }

        // Idle capture nudge: the session used tools but wrote nothing to nt.
        // A one-time, user-facing toast — never injected into the model context.
        if (CONFIG.idleNudge && event.type === "session.idle") {
          const sid: string =
            event.properties?.sessionID || event.properties?.sessionId || event.properties?.info?.id || ""
          if (
            sid &&
            sessionsWithToolUse.has(sid) &&
            !sessionsWithNtWrites.has(sid) &&
            !sessionsNudged.has(sid)
          ) {
            capped(sessionsNudged).add(sid)
            await (client as any)?.tui
              ?.showToast?.({
                body: {
                  message: "nt: nothing captured this session — run /learn to review & save learnings",
                  variant: "info",
                },
              })
              ?.catch?.(() => {})
          }
        }

        // Optional passive todo capture (off by default — see CONFIG.mirrorTodos).
        if (CONFIG.mirrorTodos && event.type === "todo.updated") {
          const todos: any[] =
            event.properties?.todos || event.properties?.todo || event.properties?.items || []
          for (const td of todos) {
            const text: string = (td?.content || td?.title || td?.text || "").trim()
            const status: string = td?.status || ""
            if (!text) continue
            if (status === "completed" || status === "done") continue
            // Best-effort one-shot add; nt itself is the source of truth for dedup
            // via content. Kept intentionally simple.
            await nt(["add", text, "--source", "opencode"])
          }
        }
      } catch {
        /* swallow — passive hooks must never throw */
      }
    },
  }
}

export default NtMemory
