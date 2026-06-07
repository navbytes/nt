#!/usr/bin/env bash
# Install the latest nt release binary — no Go, no checkout needed.
#
#   curl -fsSL https://raw.githubusercontent.com/navbytes/nt/main/install.sh | bash
#
# Overrides: NT_INSTALL_DIR (default ~/.local/bin), NT_VERSION (default: latest).
# (Contributors building from a checkout want `make install` instead.)
set -euo pipefail

REPO="navbytes/nt"
INSTALL_DIR="${NT_INSTALL_DIR:-$HOME/.local/bin}"

# --- detect platform -----------------------------------------------------
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux | darwin) ;;
  *) echo "error: unsupported OS '$os' — use 'go install $REPO@latest' instead" >&2; exit 1 ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) echo "error: unsupported arch '$arch' — use 'go install $REPO@latest' instead" >&2; exit 1 ;;
esac

# --- resolve the version -------------------------------------------------
version="${NT_VERSION:-}"
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name":' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
fi
if [ -z "$version" ]; then
  echo "error: could not find a release — set NT_VERSION, or use 'go install $REPO@latest'" >&2
  exit 1
fi
num="${version#v}" # asset names drop the leading v (nt_0.1.0_darwin_arm64.tar.gz)

# --- download, verify, install ------------------------------------------
asset="nt_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading nt $version ($os/$arch) ..."
curl -fsSL "$base/$asset" -o "$tmp/$asset" || { echo "error: download failed: $base/$asset" >&2; exit 1; }

if curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    sumtool="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    sumtool="shasum -a 256"
  else
    sumtool=""
  fi
  if [ -n "$sumtool" ]; then
    if (cd "$tmp" && grep " ${asset}\$" checksums.txt | $sumtool -c -) >/dev/null 2>&1; then
      echo "checksum verified"
    else
      echo "error: checksum verification failed" >&2
      exit 1
    fi
  fi
fi

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/nt" "$INSTALL_DIR/nt"
echo "Installed nt $version -> $INSTALL_DIR/nt"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH to run 'nt' from anywhere" ;;
esac
