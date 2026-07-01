#!/usr/bin/env bash
#
# Install the nt ↔ OpenCode memory system into your local OpenCode config.
#
# It is idempotent and safe to re-run. It will:
#   1. register nt's MCP server with OpenCode (absolute path)        → mcp.nt
#   2. copy the nt-memory plugin into ~/.config/opencode/plugins/
#   3. copy the `nt` skill into     ~/.config/opencode/skills/nt/
#   4. install a tiny AGENTS.md      (only if you don't already have one)
#   5. merge permission.skill.nt=allow into opencode.json
#   6. seed the rules/ and memory/ nt folders and an initial export
#
# Usage:  ./install.sh            # global install (~/.config/opencode)
#         NT_BIN=/path/to/nt ./install.sh
#
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cfg="${XDG_CONFIG_HOME:-$HOME/.config}/opencode"
nt="${NT_BIN:-nt}"

if ! command -v "$nt" >/dev/null 2>&1; then
  echo "error: nt not found on PATH (set NT_BIN=/abs/path/to/nt)." >&2
  exit 1
fi

echo "→ OpenCode config dir: $cfg"
mkdir -p "$cfg/plugins" "$cfg/skills/nt"

# 1. Register the MCP server (writes the absolute nt path, idempotent).
echo "→ registering nt MCP server with OpenCode"
"$nt" mcp install --client opencode

# 2 + 3. Plugin and skill.
echo "→ installing plugin  → $cfg/plugins/nt-memory.ts"
cp "$here/plugins/nt-memory.ts" "$cfg/plugins/nt-memory.ts"
echo "→ installing skill   → $cfg/skills/nt/SKILL.md"
cp "$here/skills/nt/SKILL.md" "$cfg/skills/nt/SKILL.md"

# 4. AGENTS.md — never clobber an existing one.
if [ -f "$cfg/AGENTS.md" ]; then
  echo "→ keeping your existing $cfg/AGENTS.md (see integrations/opencode/AGENTS.md to merge)"
else
  echo "→ installing AGENTS.md → $cfg/AGENTS.md"
  cp "$here/AGENTS.md" "$cfg/AGENTS.md"
fi

# 5. Merge permission.skill.nt = allow into opencode.json without touching the
#    rest. Uses node if available; otherwise prints a manual hint.
ocjson="$cfg/opencode.json"
if command -v node >/dev/null 2>&1; then
  echo "→ ensuring permission.skill.nt = allow in $ocjson"
  node - "$ocjson" <<'NODE'
const fs = require("fs");
const p = process.argv[2];
let root = {};
try { root = JSON.parse(fs.readFileSync(p, "utf8") || "{}"); } catch { root = {}; }
root["$schema"] ||= "https://opencode.ai/config.json";
root.permission ||= {};
root.permission.skill ||= {};
root.permission.skill.nt ||= "allow";
fs.writeFileSync(p, JSON.stringify(root, null, 2) + "\n");
NODE
else
  echo "  (node not found — add \"permission\": { \"skill\": { \"nt\": \"allow\" } } to $ocjson by hand)"
fi

# 6. Seed the always-in-context folders + an initial rules export. These notes are
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
    --folder rules --tag rule --source opencode >/dev/null || true
fi
if is_empty_folder memory; then
  "$nt" note "Project + user facts the agent should always know" \
    --description "Durable user preferences and project conventions (edit me)" \
    --body "Edit this note (or add siblings tagged memory-core) with durable preferences and conventions." \
    --folder memory --tag memory-core --source opencode >/dev/null || true
fi

# Initial export so file-mode users have nt-rules.md immediately (harmless in the
# default system-injection mode).
"$nt" export --tag rule --title "Rules" --out "$cfg/nt-rules.md" >/dev/null || true

cat <<EOF

✓ Done. Restart OpenCode (or reload MCP) to pick up nt.

Next:
  • Edit your rules:   nt note "<rule>" --folder rules  --tag rule
  • Edit core memory:  nt note "<fact>" --folder memory --tag memory-core
  • Everything else is normal nt notes, retrieved on demand via the nt_* tools.
  • Inspect what gets injected:  nt export --tag rule --title Rules

Modes (env on the OpenCode process, e.g. via the plugin):
  NT_INJECT=system  (default) inject live into the system prompt
  NT_INJECT=file    refresh $cfg/nt-rules.md and load it via "instructions"
  NT_INJECT=off     rely on AGENTS.md + on-demand MCP only
EOF
