/**
 * nt-memory — wire nt into Pi as the rules + memory backend.
 *
 * Pi has no built-in MCP. Its recommended path for typed tools is an in-process
 * TypeScript extension (see the Pi docs: "No MCP. …build an extension that adds
 * MCP support"). So this extension does four things, all wrapped so a missing or
 * broken nt can never take down a session:
 *
 * 1. **Native nt tools via an MCP bridge.** nt already ships an MCP server
 *    (`nt mcp`, newline-delimited JSON-RPC over stdio). On load we spawn it,
 *    list its tools, and register EACH one as a native Pi tool with `pi.registerTool`
 *    — `nt_index`, `nt_recall`, `nt_search`, `nt_get`, `nt_add`, `nt_note`,
 *    `nt_note_edit`, `nt_status`, `nt_update`, `nt_links`, and the curation tools.
 *    The tool names + schemas come straight from `nt mcp`, so they stay in sync
 *    with the binary and match the skill / prompts / AGENTS.md verbatim. This is
 *    the Pi analog of OpenCode's `nt mcp install --client opencode`.
 *
 * 2. **Always-in-context injection.** The user's nt **rules** + a small **core
 *    memory** are compiled live from nt on every agent run and appended to the
 *    system prompt via the `before_agent_start` hook — so editing a note in nt
 *    updates what the agent sees, with no stale exported file.
 *
 * 3. **Error-triggered recall.** When a bash tool call fails, we run
 *    `nt recall --lessons-only` on the command + error and append any recorded
 *    lessons directly onto the failing tool's result via the `tool_result` hook,
 *    so the mistake summons its own antidote on the very next turn.
 *
 * 4. **Idle capture nudge.** If a session did real work but wrote nothing to nt,
 *    a one-time toast suggests running /learn — so sessions don't end with zero
 *    recorded learning.
 *
 * The knowledge base (everything in nt outside the small rules/memory core) is
 * NOT injected — it stays behind the bridged nt tools, fetched on demand so it
 * costs zero tokens until used.
 *
 * nt conventions this relies on (see the bundle README):
 *   - folder `rules/`  + tag `rule`        → always-in-context rules
 *   - folder `memory/` + tag `memory-core` → small evolving core memory
 *   - tag `lesson`                          → recorded mistakes, ranked first by recall
 *   - everything else                       → on-demand KB via the bridged tools
 */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"
import { execFile, spawn } from "node:child_process"

// nt binary. Override with NT_BIN if it isn't on Pi's PATH (GUI launches often
// miss ~/.local/bin).
const NT_BIN = process.env.NT_BIN || "nt"

const CONFIG = {
  // "system" (default) → inject rules+memory live via before_agent_start.
  // "off"              → don't inject; rely on AGENTS.md + the bridged tools only.
  injectMode: (process.env.NT_INJECT || "system") as "system" | "off",

  rulesTag: "rule",
  memoryTag: "memory-core",

  // Hard cap on injected characters so the always-in-context block can't blow the
  // token budget. Keep rules/memory notes short; overflow truncates on note
  // boundaries with a marker (never mid-note).
  maxInjectChars: Number(process.env.NT_INJECT_MAX || 8000),

  // Register nt's MCP tools as native Pi tools. Disable with NT_BRIDGE=0 to run
  // memory-injection-only (the agent then drives nt via the CLI over bash).
  bridge: process.env.NT_BRIDGE !== "0",

  // On a failed bash call, `nt recall --lessons-only` the command + error and
  // append matching lessons onto the result. Disable with NT_ERROR_RECALL=0.
  errorRecall: process.env.NT_ERROR_RECALL !== "0",

  // One-time toast when a session that used tools ends without a single nt write.
  // Disable with NT_IDLE_NUDGE=0.
  idleNudge: process.env.NT_IDLE_NUDGE !== "0",

  // Per-request timeout for a bridged tool call (ms).
  callTimeoutMs: Number(process.env.NT_BRIDGE_TIMEOUT || 20000),
}

// nt write tools, matched exactly (Pi registers the tools under nt's own names).
const NT_WRITE_TOOL = /^nt_(add|note|note_edit|update|tag|mv|rm|archive|relink)$/

// ---- run an nt subcommand, returning stdout ("" on any failure) ----
function runNt(args: string[]): Promise<string> {
  return new Promise((resolve) => {
    try {
      execFile(NT_BIN, args, { maxBuffer: 8 * 1024 * 1024 }, (err, stdout) => {
        resolve(err ? "" : String(stdout ?? ""))
      })
    } catch {
      resolve("")
    }
  })
}

async function runNtJSON(args: string[]): Promise<any> {
  const out = await runNt(args)
  if (!out.trim()) return null
  try {
    return JSON.parse(out)
  } catch {
    return null
  }
}

// ---- minimal MCP stdio client for `nt mcp` (newline-delimited JSON-RPC) ----
type Pending = { resolve: (v: any) => void; reject: (e: any) => void }

class NtBridge {
  private proc: ReturnType<typeof spawn> | null = null
  private buf = ""
  private nextId = 1
  private pending = new Map<number, Pending>()
  private dead = false
  tools: any[] = []

  async start(): Promise<void> {
    this.proc = spawn(NT_BIN, ["mcp"], { stdio: ["pipe", "pipe", "ignore"] })
    this.proc.on("exit", () => this.die(new Error("nt mcp exited")))
    this.proc.on("error", (e) => this.die(e))
    this.proc.stdout?.on("data", (d: Buffer) => this.onData(d))

    await this.request("initialize", {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: { name: "pi-nt-memory", version: "1" },
    })
    // Best-effort per the MCP handshake; nt ignores notifications.
    this.notify("notifications/initialized")
    const res = await this.request("tools/list", {})
    this.tools = Array.isArray(res?.tools) ? res.tools : []
  }

  private onData(chunk: Buffer) {
    this.buf += chunk.toString()
    let nl: number
    while ((nl = this.buf.indexOf("\n")) >= 0) {
      const line = this.buf.slice(0, nl).trim()
      this.buf = this.buf.slice(nl + 1)
      if (!line) continue
      let msg: any
      try {
        msg = JSON.parse(line)
      } catch {
        continue
      }
      if (msg && msg.id != null && this.pending.has(msg.id)) {
        const p = this.pending.get(msg.id)!
        this.pending.delete(msg.id)
        if (msg.error) p.reject(new Error(msg.error.message || "nt mcp error"))
        else p.resolve(msg.result)
      }
    }
  }

  private die(err: any) {
    if (this.dead) return
    this.dead = true
    for (const p of this.pending.values()) p.reject(err)
    this.pending.clear()
  }

  private write(obj: any) {
    if (this.dead || !this.proc?.stdin) throw new Error("nt bridge is down")
    this.proc.stdin.write(JSON.stringify(obj) + "\n")
  }

  private notify(method: string, params?: any) {
    try {
      this.write({ jsonrpc: "2.0", method, params: params ?? {} })
    } catch {
      /* ignore — notifications are best-effort */
    }
  }

  request(method: string, params: any): Promise<any> {
    return new Promise((resolve, reject) => {
      const id = this.nextId++
      this.pending.set(id, { resolve, reject })
      try {
        this.write({ jsonrpc: "2.0", id, method, params: params ?? {} })
      } catch (e) {
        this.pending.delete(id)
        reject(e)
        return
      }
      setTimeout(() => {
        if (this.pending.has(id)) {
          this.pending.delete(id)
          reject(new Error("nt mcp timeout"))
        }
      }, CONFIG.callTimeoutMs)
    })
  }

  call(name: string, args: any): Promise<any> {
    return this.request("tools/call", { name, arguments: args ?? {} })
  }

  stop() {
    try {
      this.proc?.kill()
    } catch {
      /* ignore */
    }
    this.die(new Error("stopped"))
  }
}

// Compile the always-in-context block: rules first, then core memory. Each is a
// separate `nt export` (tag filters are AND-combined, so two tags need two
// calls). Provenance AND the top "Generated by" header are dropped to save
// tokens on every turn — the agent can still nt_search/nt_get the source note.
async function compile(): Promise<string> {
  const [rules, memory] = await Promise.all([
    runNt(["export", "--tag", CONFIG.rulesTag, "--title", "Rules", "--no-provenance", "--no-header"]),
    runNt(["export", "--tag", CONFIG.memoryTag, "--title", "Memory", "--no-provenance", "--no-header"]),
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

// Recall recorded lessons for a failed command; empty string when none match.
async function recallLessons(command: string, errorTail: string): Promise<string> {
  const query = [command.split("\n")[0].slice(0, 120), errorTail.slice(0, 160)]
    .filter(Boolean)
    .join(" ")
    .trim()
  if (!query) return ""
  const stubs = await runNtJSON(["recall", query, "--lessons-only", "--json", "--limit", "3"])
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

// Flatten a Pi result's content blocks to plain text (for scanning bash errors).
function blocksToText(content: any): string {
  if (!Array.isArray(content)) return typeof content === "string" ? content : ""
  return content
    .map((b: any) => (b && typeof b.text === "string" ? b.text : ""))
    .filter(Boolean)
    .join("\n")
}

export default async function (pi: ExtensionAPI) {
  // ---- per-session state, reset on session_start (a Pi process can host
  // several sessions via /new and /fork) ----
  let usedTools = false
  let wroteNt = false
  let nudged = false
  const recalledFailures = new Set<string>() // throttle: one recall per distinct failing command

  // ---- 1. MCP bridge: register nt's tools as native Pi tools ----
  if (CONFIG.bridge) {
    try {
      const bridge = new NtBridge()
      await bridge.start()
      for (const tool of bridge.tools) {
        if (!tool?.name) continue
        try {
          pi.registerTool({
            name: tool.name,
            label: String(tool.name).replace(/_/g, " "),
            description: tool.description || "",
            parameters: tool.inputSchema || { type: "object", properties: {} },
            async execute(_toolCallId: string, params: unknown) {
              try {
                const res = await bridge.call(tool.name, params ?? {})
                const content = Array.isArray(res?.content)
                  ? res.content
                  : [{ type: "text", text: String(res ?? "") }]
                return { content, details: res?.isError ? { isError: true } : undefined }
              } catch (e: any) {
                return {
                  content: [
                    {
                      type: "text",
                      text: `nt: ${tool.name} failed (${e?.message || e}). Fall back to the nt CLI via bash (e.g. \`nt ${String(
                        tool.name,
                      ).replace(/^nt_/, "")} …\`).`,
                    },
                  ],
                }
              }
            },
          })
        } catch {
          /* a tool that won't register is skipped — the rest still work */
        }
      }
      // Tear the subprocess down when the session ends.
      pi.on("session_shutdown", async () => bridge.stop())
    } catch {
      /* bridge unavailable — the agent falls back to the nt CLI (see the skill/AGENTS.md) */
    }
  }

  // ---- 2. Always-in-context injection via the system prompt ----
  // The returned systemPrompt CHAINS (appends to the current state), so build on
  // event.systemPrompt. Stable within a session and cache-friendly.
  pi.on("before_agent_start", async (event: any) => {
    if (CONFIG.injectMode !== "system") return
    try {
      const text = await compile()
      if (!text) return
      return {
        systemPrompt:
          `${event.systemPrompt}\n\n` +
          `<nt-memory source="nt store — edit notes in nt, not here">\n${text}\n</nt-memory>`,
      }
    } catch {
      /* never break a session over memory injection */
    }
  })

  // ---- 3. Error-triggered recall + write/tool-use tracking ----
  pi.on("tool_result", async (event: any) => {
    try {
      const tool: string = event?.toolName || ""
      usedTools = true
      if (NT_WRITE_TOOL.test(tool)) wroteNt = true

      if (!CONFIG.errorRecall || tool !== "bash" || !event?.isError) return
      const command = String(event?.input?.command ?? "")
      const throttleKey = command.slice(0, 80)
      if (!throttleKey || recalledFailures.has(throttleKey)) return
      if (recalledFailures.size > 128) recalledFailures.clear()
      recalledFailures.add(throttleKey)

      const errorTail =
        blocksToText(event?.content)
          .trim()
          .split("\n")
          .filter((l: string) => l.trim())
          .slice(-2)
          .join(" ") || ""
      const block = await recallLessons(command, errorTail)
      if (!block) return
      // Append the lessons onto the failing result the model reads next turn.
      return { content: [...(Array.isArray(event?.content) ? event.content : []), { type: "text", text: block }] }
    } catch {
      /* passive hooks must never throw */
    }
  })

  // ---- 4. Idle capture nudge (once per session) ----
  pi.on("agent_end", async (_event: any, ctx: any) => {
    try {
      if (!CONFIG.idleNudge || nudged || !usedTools || wroteNt) return
      nudged = true
      ctx?.ui?.notify?.(
        "nt: nothing captured this session — run /learn to review & save learnings",
        "info",
      )
    } catch {
      /* the nudge is best-effort */
    }
  })

  // Reset per-session flags so /new and /fork start clean.
  pi.on("session_start", async () => {
    usedTools = false
    wroteNt = false
    nudged = false
    recalledFailures.clear()
  })
}
