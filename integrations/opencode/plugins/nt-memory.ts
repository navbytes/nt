/**
 * nt-memory — wire nt into OpenCode as the rules + memory backend.
 *
 * What it does
 * ------------
 * 1. Injects an always-in-context block (your nt **rules** + a small **core
 *    memory**) into the system prompt at assembly time, recompiled live from nt
 *    on every session — so editing a note in nt updates what the agent sees, with
 *    no stale exported file. Uses the `experimental.chat.system.transform` hook.
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
 * nt_get / nt_links), fetched on demand so it costs zero tokens until used.
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
import os from "os"
import path from "path"

const HOME = os.homedir()

const CONFIG = {
  // nt binary. Override with NT_BIN if it isn't on OpenCode's PATH (GUI launches
  // often miss ~/.local/bin).
  ntBin: process.env.NT_BIN || "nt",

  // "system" (default) → inject live via the system-prompt transform.
  // "file"             → instead refresh a markdown file for the `instructions`
  //                      config to load (see README); set this if your OpenCode
  //                      build lacks experimental.chat.system.transform.
  // "off"              → don't inject; rely on AGENTS.md + on-demand MCP only.
  injectMode: (process.env.NT_INJECT || "system") as "system" | "file" | "off",

  rulesTag: "rule",
  memoryTag: "memory-core",

  // file-mode target; must match the path in opencode.json `instructions`.
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
const NT_WRITE_TOOL = /(^|_)nt_(add|note|done|update|tag|mv|archive|supersede|relink)$/

export const NtMemory: Plugin = async ({ $, client }) => {
  // Run an nt subcommand, returning stdout (empty string on any failure).
  const nt = async (args: string[]): Promise<string> => {
    try {
      return await $`${CONFIG.ntBin} ${args}`.text()
    } catch {
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

  // Compile the always-in-context block: rules first, then core memory. Each is a
  // separate `nt export` (tag filters are AND-combined, so two tags need two
  // calls). Provenance AND the top "Generated by" header are dropped to save
  // tokens on every turn — the agent can still nt_search/nt_get the source note.
  const compile = async (): Promise<string> => {
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
        const room = Math.max(0, max - used - 48)
        kept.push(b.slice(0, room).trimEnd() + "\n<!-- nt: rule truncated to fit NT_INJECT_MAX -->")
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
      result += `\n\n<!-- nt: ${omitted} note(s) omitted to fit NT_INJECT_MAX=${max}; trim rules/memory notes -->`
    }
    return result
  }

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
      if (CONFIG.injectMode !== "system" && pendingLessons.size === 0) return
      try {
        if (!Array.isArray(output?.system)) return
        if (CONFIG.injectMode === "system") {
          const text = await compile()
          if (text) {
            output.system.push(
              `<nt-memory source="nt store — edit notes in nt, not here">\n${text}\n</nt-memory>`,
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
        if (sid) {
          capped(sessionsWithToolUse).add(sid)
          if (NT_WRITE_TOOL.test(tool)) capped(sessionsWithNtWrites).add(sid)
        }

        if (!CONFIG.errorRecall || tool !== "bash") return
        if (!failedExit(output?.metadata)) return
        const command: string = String(input?.args?.command ?? "")
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
        // File mode: refresh the instructions-backed file at session start.
        if (CONFIG.injectMode === "file" && event.type === "session.created") {
          await nt([
            "export",
            "--tag",
            CONFIG.rulesTag,
            "--title",
            "Rules",
            "--out",
            CONFIG.instructionsFile,
          ])
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
                  message: "nt: nothing captured this session — worth saving a note or lesson? (nt_note / nt_add)",
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
