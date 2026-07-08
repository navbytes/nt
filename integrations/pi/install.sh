#!/usr/bin/env bash
#
# Install the nt ↔ Pi memory system into your local Pi config.
#
# NOTE: `nt pi install` (built into the binary since the integration bundle was
# embedded) performs these same steps without a repo checkout, on every
# platform — prefer it. This script remains for repo-checkout installs (e.g.
# iterating on the extension without rebuilding nt).
#
# Pi has no built-in MCP; the nt-memory extension bridges nt's MCP server in as
# native Pi tools, so there's no separate server-registration step.
#
# It is idempotent and safe to re-run. It will:
#   1. copy the nt-memory extension into ~/.pi/agent/extensions/
#   2. copy the `nt` skill into        ~/.pi/agent/skills/nt/
#   3. copy the /learn + /recall prompt templates into ~/.pi/agent/prompts/
#   4. install a tiny AGENTS.md         (only if you don't already have one)
#   5. seed the rules/ and memory/ nt folders and an initial export
#
# Usage:  ./install.sh            # global install (~/.pi/agent)
#         NT_BIN=/path/to/nt ./install.sh
#         PI_CODING_AGENT_DIR=/custom/dir ./install.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cfg="${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}"
nt="${NT_BIN:-nt}"

if ! command -v "$nt" >/dev/null 2>&1; then
  echo "error: nt not found on PATH (set NT_BIN=/abs/path/to/nt)." >&2
  exit 1
fi

echo "→ Pi config dir: $cfg"
mkdir -p "$cfg/extensions" "$cfg/skills/nt" "$cfg/prompts"

# 1–3. Extension, skill, and the /learn + /recall prompt templates.
echo "→ installing extension → $cfg/extensions/nt-memory.ts"
cp "$here/extensions/nt-memory.ts" "$cfg/extensions/nt-memory.ts"
echo "→ installing skill     → $cfg/skills/nt/SKILL.md"
cp "$here/skills/nt/SKILL.md" "$cfg/skills/nt/SKILL.md"
echo "→ installing prompt    → $cfg/prompts/learn.md   (/learn — session review & capture)"
cp "$here/prompts/learn.md" "$cfg/prompts/learn.md"
echo "→ installing prompt    → $cfg/prompts/recall.md  (/recall — on-demand memory brief)"
cp "$here/prompts/recall.md" "$cfg/prompts/recall.md"

# 4. AGENTS.md — never clobber an existing one.
if [ -f "$cfg/AGENTS.md" ]; then
  echo "→ keeping your existing $cfg/AGENTS.md (see integrations/pi/AGENTS.md to merge)"
else
  echo "→ installing AGENTS.md → $cfg/AGENTS.md"
  cp "$here/AGENTS.md" "$cfg/AGENTS.md"
fi

# 5. Seed the always-in-context folders + an initial rules export. These notes are
#    examples you can edit or delete; the folders are what matter.
#    Guard on the JSON empty-array marker: `nt notes --folder X` prints the human
#    string "no notes" (non-empty) on an empty store, so a `[ -z ... ]` test is
#    FALSE and would SKIP seeding on the very first run — shipping an empty injected
#    block. `--json` returns "[]" on an empty folder, which we can test reliably.
echo "→ seeding nt rules/ and memory/ folders (examples — edit or remove)"
is_empty_folder() { [ "$("$nt" notes --folder "$1" --json 2>/dev/null | tr -d '[:space:]')" = "[]" ]; }
if is_empty_folder rules; then
  "$nt" note "Output style: terse factual bullets" \
    --description "How the agent should phrase answers by default" \
    --body "- Answer in bullet points, not prose.
- Plain, direct words. No filler, hedging, or fancy phrasing.
- Lead with the fact/answer; skip preamble and restating the question.
- Elaborate only when asked." \
    --folder rules --tag rule --source pi >/dev/null || true
fi
if is_empty_folder memory; then
  "$nt" note "Project + user facts the agent should always know" \
    --description "Durable user preferences and project conventions (edit me)" \
    --body "Edit this note (or add siblings tagged memory-core) with durable preferences and conventions." \
    --folder memory --tag memory-core --source pi >/dev/null || true
fi

# Initial export so file-mode users have nt-rules.md immediately (harmless in the
# default system-injection mode).
"$nt" export --tag rule --title "Rules" --out "$cfg/nt-rules.md" >/dev/null || true

cat <<EOF

✓ Done. Restart Pi (or run /reload) to pick up the nt-memory extension.

Next:
  • Edit your rules:   nt note "<rule>" --kind rule --description "…"
  • Edit core memory:  nt note "<fact>" --kind memory --description "…"
  • Everything else is normal nt notes, retrieved on demand via the nt_* tools.
  • Inspect what gets injected:  nt export --tag rule --title Rules

Modes (env on the Pi process):
  NT_INJECT=system  (default) inject rules+memory live into the system prompt
  NT_INJECT=off     rely on AGENTS.md + on-demand tools only
  NT_BRIDGE=0       skip registering nt's tools (injection-only; drive nt via bash)

Learning-loop automation (all ON by default; set =0 to disable):
  NT_ERROR_RECALL   failed bash → append matching nt lessons onto the result
  NT_IDLE_NUDGE     toast when a session ends without capturing anything
EOF
