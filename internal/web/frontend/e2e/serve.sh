#!/usr/bin/env bash
# Playwright webServer command: build nt (embedding the freshly-built dist),
# seed a throwaway store, and serve the SPA on :4173. Run `npm run build` first
# so the embedded bundle reflects the current source.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
BIN="$ROOT/nt-e2e"

go build -C "$ROOT" -o "$BIN" .

NT_DIR="$(mktemp -d)"
export NT_DIR

"$BIN" add "ship the SPA" --pri high >/dev/null
"$BIN" add "review graph view" >/dev/null
"$BIN" note "Welcome" --body $'# Welcome\n\nSee [[Design]] for details.' >/dev/null
"$BIN" note "Design" --body $'# Design\n\n## Goals\n\nRefs [[Welcome]].\n\n## Non-goals\n\nlater.' >/dev/null

exec "$BIN" web --edit --host 127.0.0.1 --port 4173
