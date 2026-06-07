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

	"github.com/wailsapp/wails/v2"
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
	// srv.SetEdit(true) // uncomment to allow in-app editing (CSRF-guarded)

	// Hand the existing handler to Wails. No TCP port is opened — the native
	// webview talks to the Go handler in-process.
	err = wails.Run(&options.App{
		Title:            "nt",
		Width:            1200,
		Height:           820,
		MinWidth:         640,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 0x1b, G: 0x26, B: 0x3b, A: 1}, // Tokyo Night base
		AssetServer: &assetserver.Options{
			Handler: srv.Handler(),
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
		},
	})
	if err != nil {
		log.Fatalf("nt-desktop: %v", err)
	}
}
