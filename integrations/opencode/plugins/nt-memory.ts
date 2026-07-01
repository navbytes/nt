/**
 * nt-memory — wire nt into OpenCode as the rules + memory backend.
 *
 * What it does
 * ------------
 * 1. Injects an always-in-context block (your nt **rules** + a small **core
 *    memory**) into the system prompt at assembly time, recompiled live from nt
 *    on every session — so editing a note in nt updates what the agent sees, with
 *    no stale exported file. Uses the `experimental.chat.system.transform` hook.
 * 2. (Optional, off by default) keeps a static markdown file fresh so the
 *    documented `instructions` config path works on clients/versions without the
 *    experimental hook. Toggle with NT_INJECT=file.
 * 3. (Optional, off by default) mirrors OpenCode's todo list into nt tasks on
 *    `todo.updated` — the OpenCode analog of Claude Code's `nt hook`.
 *
 * The knowledge base (everything in nt outside the small rules/memory core) is
 * NOT injected — it stays behind the nt MCP tools (nt_search / nt_recall /
 * nt_links), fetched on demand so it costs zero tokens until used. Register those
 * once with:  nt mcp install --client opencode
 *
 * Everything here is wrapped so a missing or broken nt can never break a session.
 *
 * nt conventions this relies on (see the bundle README):
 *   - folder `rules/`  + tag `rule`        → always-in-context rules
 *   - folder `memory/` + tag `memory-core` → small evolving core memory
 *   - everything else                      → on-demand KB via the MCP tools
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

  // Mirror OpenCode todos → nt tasks. Off by default: the agent already captures
  // tasks via the nt_add MCP tool, and a one-way mirror without dedup can double
  // up. Enable with NT_MIRROR_TODOS=1 if you prefer passive capture.
  mirrorTodos: process.env.NT_MIRROR_TODOS === "1",
}

export const NtMemory: Plugin = async ({ $ }) => {
  // Run an nt subcommand, returning stdout (empty string on any failure).
  const nt = async (args: string[]): Promise<string> => {
    try {
      return await $`${CONFIG.ntBin} ${args}`.text()
    } catch {
      return ""
    }
  }

  // Compile the always-in-context block: rules first, then core memory. Each is a
  // separate `nt export` (tag filters are AND-combined, so two tags need two
  // calls). Provenance comments are dropped to save tokens — the agent can still
  // nt_search/nt_recall the source note by content.
  const compile = async (): Promise<string> => {
    const [rules, memory] = await Promise.all([
      nt(["export", "--tag", CONFIG.rulesTag, "--title", "Rules", "--no-provenance"]),
      nt(["export", "--tag", CONFIG.memoryTag, "--title", "Memory", "--no-provenance"]),
    ])
    let out = [rules.trim(), memory.trim()].filter(Boolean).join("\n\n")
    if (out.length > CONFIG.maxInjectChars) {
      out = out.slice(0, CONFIG.maxInjectChars) + "\n\n<!-- nt: truncated; trim rules/memory notes -->"
    }
    return out
  }

  return {
    // Inject live, every session, as the system prompt is built.
    "experimental.chat.system.transform": async (_input: any, output: any) => {
      if (CONFIG.injectMode !== "system") return
      try {
        const text = await compile()
        if (text && Array.isArray(output?.system)) {
          output.system.push(
            `<nt-memory source="nt store — edit notes in nt, not here">\n${text}\n</nt-memory>`,
          )
        }
      } catch {
        /* never break a session over memory injection */
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
