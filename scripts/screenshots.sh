#!/usr/bin/env bash
# Generate the README TUI screenshots: dump each view's ANSI from the render
# harness, then turn each into a PNG with charmbracelet/freeze.
#
#   ./scripts/screenshots.sh
#
# Requires: go, and `freeze` (go install github.com/charmbracelet/freeze@latest).
set -euo pipefail

cd "$(dirname "$0")/.."
DIR=docs/screenshots

command -v freeze >/dev/null 2>&1 || { echo "freeze not found — go install github.com/charmbracelet/freeze@latest"; exit 1; }

echo "▸ dumping ANSI frames…"
NT_SCREENSHOTS=1 go test ./internal/tui/ -run TestRenderScreenshots >/dev/null

echo "▸ freezing PNGs…"
shopt -s nullglob
for f in "$DIR"/*.ansi; do
  png="${f%.ansi}.png"
  freeze "$f" -o "$png" \
    --window \
    --background "#1a1b26" \
    --border.radius 8 --border.width 1 --border.color "#3b4261" \
    --padding 20 --margin 0 \
    --font.family "JetBrains Mono" --font.size 14 --line-height 1.3
  echo "  $png"
done
rm -f "$DIR"/*.ansi
echo "✓ wrote PNGs to $DIR"
