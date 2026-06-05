#!/usr/bin/env bash
# Build nt from source and install it. Requires Go 1.22+.
#   curl -fsSL .../install.sh | bash   (from a checkout: ./install.sh)
# Override the destination with NT_INSTALL_DIR (default ~/.local/bin).
set -euo pipefail

INSTALL_DIR="${NT_INSTALL_DIR:-$HOME/.local/bin}"
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required (https://go.dev/dl/)" >&2
  exit 1
fi

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
echo "Building nt $VERSION ..."
go build -ldflags "-s -w -X main.version=$VERSION" -o nt .

mkdir -p "$INSTALL_DIR"
install -m 0755 nt "$INSTALL_DIR/nt"
echo "Installed nt -> $INSTALL_DIR/nt"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH to run 'nt' from anywhere" ;;
esac
