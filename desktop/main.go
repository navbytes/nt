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
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/web"
)

func main() {
	// Open the same store + mutation engine the CLI/TUI/MCP use.
	eng, err := mutate.Open()
	if err != nil {
		log.Fatalf("nt-desktop: open store: %v", err)
	}

	// Build the exact server `nt web` builds.
	srv, err := web.NewServer(eng, "desktop")
	if err != nil {
		log.Fatalf("nt-desktop: build server: %v", err)
	}
	srv.StartWatch() // live-reload when the store changes on disk

	// Editing on: the desktop app gets the full web UX — quick-add with parse
	// preview, complete/reschedule/undo, the CodeMirror editor. The trust model
	// is stronger than `nt web --edit`: no TCP port is even opened (the webview
	// talks to the Go handler in-process), and the CSRF guard still applies.
	srv.SetEdit(true)

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
		Title:            "nt",
		Width:            1280,
		Height:           860,
		MinWidth:         640,
		MinHeight:        480,
		Menu:             appMenu,
		BackgroundColour: &options.RGBA{R: 0x1b, G: 0x26, B: 0x3b, A: 1}, // Tokyo Night base (pre-paint flash)
		AssetServer: &assetserver.Options{
			Handler: srv.Handler(),
		},
		Mac: &mac.Options{
			// Standard title bar: hidden-inset overlays the traffic lights on the
			// sidebar's brand corner. No forced DarkAqua — the window chrome
			// follows the system appearance, matching the app's adaptive theme.
			TitleBar: mac.TitleBarDefault(),
		},
	})
	if err != nil {
		log.Fatalf("nt-desktop: %v", err)
	}
}
