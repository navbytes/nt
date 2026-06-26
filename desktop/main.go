// Command nt-desktop is a Wails spike: it wraps nt's existing web UI in a
// native window. It does NOT reimplement the UI — it constructs the same
// internal/web server the `nt web` command uses and hands its http.Handler to
// Wails' asset server, so the native webview renders the identical
// server-rendered pages (notes, search, tasks, graph, mermaid, themes).
//
// The point of the spike is to measure effort, not to ship: it proves the
// hexagonal split means a desktop app reuses the whole domain AND the whole
// web adapter, with the desktop-only dependencies (Wails, CGO/WebKit) isolated
// in this nested module so the `nt` CLI's single-static-binary story is intact.
//
// Run (no Wails CLI needed): `go run .` from this directory.
// Or, for the full dev experience with hot-reload: `wails dev`.
package main

import (
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/web"
)

// version is the app version shown in the in-app About panel. The release's
// `wails build` injects the release tag via -ldflags "-X main.version=<ver>"
// (the same way GoReleaser stamps the CLI), so a tagged desktop build reports
// the same version as `nt --version`. A local `go run` build stays "dev".
var version = "dev"

func main() {
	// WebKitGTK (the Linux webview) disables WebGL under its default DMA-BUF
	// renderer on many GPU/driver combinations, which left the 3D graph a blank
	// canvas in the desktop app. Opting out of the DMA-BUF renderer restores a
	// working WebGL context. Must be set before the webview initialises; it has
	// no effect on macOS/Windows, so it's guarded to Linux.
	if runtime.GOOS == "linux" {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}

	// Open the same store + mutation engine the CLI/TUI/MCP use.
	eng, err := mutate.Open()
	if err != nil {
		log.Fatalf("nt-desktop: open store: %v", err)
	}

	// Build the exact server `nt web` builds.
	srv, err := web.NewServer(eng, version)
	if err != nil {
		log.Fatalf("nt-desktop: build server: %v", err)
	}
	srv.StartWatch() // live-reload when the store changes on disk

	// The desktop app gets the full web UX — quick-add with parse preview,
	// complete/reschedule/undo, the CodeMirror editor. Editing is always on (as it
	// now is for `nt web`); the trust model here is even stronger — no TCP port is
	// opened (the webview talks to the Go handler in-process), and the per-process
	// CSRF guard still applies to every write.

	// On macOS a webview gets working ⌘C/⌘V/⌘X/⌘A (and ⌘Q) only if the app has
	// an Edit menu — without one the clipboard shortcuts are dead keys. Other
	// platforms have native shortcuts, and a visible menu bar would eat a row.
	var appMenu *menu.Menu
	if runtime.GOOS == "darwin" {
		appMenu = menu.NewMenu()
		appMenu.Append(menu.AppMenu())
		appMenu.Append(menu.EditMenu())
	}

	// Hand the existing handler to Wails. No TCP port is opened — the native
	// webview talks to the Go handler in-process.
	err = wails.Run(&options.App{
		Title:     "nt",
		Width:     1280,
		Height:    860,
		MinWidth:  640,
		MinHeight: 480,
		Menu:      appMenu,
		// Neutral pre-paint backing (shown for one frame before the webview
		// renders). Window vibrancy masks it; a light neutral is least jarring.
		BackgroundColour: &options.RGBA{R: 0xEC, G: 0xEC, B: 0xEC, A: 1},
		AssetServer: &assetserver.Options{
			Handler: srv.Handler(),
		},
		Mac: &mac.Options{
			// Hidden-inset title bar: the traffic lights inset over the sidebar's
			// top corner and content runs full height (FullSizeContent). The
			// frontend insets the sidebar below the lights via [data-desktop].
			TitleBar: mac.TitleBarHiddenInset(),
			// Translucent window + transparent webview install the native macOS
			// NSVisualEffectView vibrancy behind the UI; the sidebar and topbar
			// stay translucent to reveal it while the content pane paints opaque.
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			// Follow the system appearance — the app's theme is adaptive.
			Appearance: mac.DefaultAppearance,
			Preferences: &mac.Preferences{
				TabFocusesLinks: mac.Enabled,
			},
		},
	})
	if err != nil {
		log.Fatalf("nt-desktop: %v", err)
	}
}
