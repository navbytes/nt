# nt-desktop — Wails spike

A **proof-of-concept** desktop shell for nt. It wraps nt's *existing* `nt web`
UI in a native window — it does **not** reimplement anything. `main.go`
constructs the same `internal/web` server the `nt web` command uses and hands
its `http.Handler` to Wails' asset server, so the native WebKit window renders
the identical server-rendered pages (notes, search, `/tasks`, `/graph`, mermaid,
themes, command palette, live-reload).

The spike exists to **measure effort** — but it is now also **distributable**:
tagging a release builds and uploads native bundles (see below).

## Install (prebuilt)

Each `vX.Y.Z` release attaches native bundles built by the `desktop` job in
[`.github/workflows/release.yml`](../.github/workflows/release.yml):

- **macOS** — `nt_<ver>_macos_universal.zip` (universal; signed + notarized once
  the Apple secrets in [RELEASING.md](RELEASING.md) are set, otherwise ad-hoc:
  right-click → Open the first time).
- **Linux** — `nt_<ver>_linux_amd64.tar.gz` (needs `libgtk-3` +
  `libwebkit2gtk-4.1`).
- **Windows** — `nt_<ver>_windows_amd64.zip` (uses the Edge WebView2 runtime).

This is separate from the `nt` **CLI**, which installs via `go install` / curl /
Homebrew. A GUI app can't ship through `go install` (CGO + a build tag + a
WebView runtime are required), so the two have different channels by design.

## Why it's a separate module

This directory is its own Go module (`github.com/navbytes/nt/desktop`) with a
`replace => ../` back to the main repo. That keeps every desktop-only
dependency — Wails, `go-webview2`, CGO/WebKit — **out of the `nt` CLI's
`go.mod`**, so the CLI keeps its single-static-binary, no-system-deps story.
The main module's `go test ./...`, `golangci-lint`, and GoReleaser never see
this module (a nested module is invisible to the parent's `./...`).

Because Go's `internal/` visibility rule is path-prefix based (not
module-based), this module *can* still import `github.com/navbytes/nt/internal/*`
directly — so the spike runs against the real domain, not a copy.

## Run it

Wails selects its native webview behind a build tag, so a `production` (or `dev`)
tag is required — a bare `go run .` compiles a stub that exits with
*"Wails applications will not build without the correct build tags."*

```bash
cd desktop
go run -tags production .   # opens a native nt window over your real store ($NT_DIR)
```

On the macOS 15 SDK, Wails 2.12 references `UTType` without linking its
framework; `cgo_darwin.go` adds `-framework UniformTypeIdentifiers` for the
`production`/`dev` tags so no `CGO_LDFLAGS` override is needed.

For the full Wails dev experience (hot reload, devtools, `.app` bundling):

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails dev           # dev mode (sets -tags dev)
wails build         # produces a distributable nt.app (macOS) / .exe / binary
```

Prereqs for `wails build`: the Wails CLI and a WebKit/WebView2 runtime
(macOS ships WebKit; Linux needs `libgtk` + `libwebkit2gtk`; Windows uses the
Edge WebView2 runtime). `wails doctor` checks your machine.

## What the spike proved

- **Effort is low.** A working native window over the *entire* current UI is
  ~60 lines of `main.go` plus one tiny exported method on the web package
  (`Server.Handler()`). No UI was rewritten.
- **The hexagonal split pays off.** The desktop app reuses the whole domain
  *and* the whole web adapter. The only seam needed was exposing the existing
  `http.Handler`.
- **The CLI stays pure.** `grep -i wails ../go.mod` is empty; the CLI still
  builds, vets, and tests unchanged. The 97-line Wails dependency tree lives
  only in `desktop/go.sum`.
- **It compiles and links natively.** `go build -tags production` produces an
  18 MB Mach-O arm64 binary linked against `WebKit.framework`, `Cocoa`, and
  `AppKit` (verified with `otool -L`).

## What this tells the framework decision

Today's **server-rendered** UI drops into Wails *as-is* via
`assetserver.Options{Handler: ...}` — so shipping a desktop app does **not**
require an SPA-framework rewrite first. A future, richer desktop app could
instead bind Go methods to a client-rendered frontend (Wails' native model),
but that is an *additive* evolution, not a prerequisite. See
[`../docs/adr/0001-web-frontend-and-desktop.md`](../docs/adr/0001-web-frontend-and-desktop.md).

## Limitations (it's a spike)

- No Go↔JS bindings yet — the window is the web UI verbatim; it doesn't call
  native methods.
- Release bundles are built in CI, but the macOS app is only signed/notarized
  once the Apple secrets ([RELEASING.md](RELEASING.md)) are configured.
- Built/tested on macOS; the Linux/Windows matrix legs are wired but need a real
  tagged run (and their WebView runtimes) to exercise end-to-end.
- SSE live-reload works in the webview; the editor (`SetEdit(true)`) is left
  off by default.
