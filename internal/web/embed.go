package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"strings"
)

func init() {
	// The Go file server serves the embedded SPA via mime.TypeByExtension; the
	// PWA manifest extension isn't registered by default, so register it (and pin
	// the SW + favicon types) so browsers accept them.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
}

// distFS holds the built Svelte SPA (internal/web/frontend/dist), embedded into
// the binary so `nt web --spa` is still a single static binary with no runtime
// dependency. The bundle is a build artifact: it must be built (`make web-build`)
// and committed so `go build`/`go install` — which never run npm — still embed a
// current UI. `all:` includes files Vite may emit with leading underscores/dots.
//
//go:embed all:frontend/dist
var distFS embed.FS

// spaRoutes serves the embedded SPA: hashed assets under /assets/ (immutable),
// and index.html for every other path so the client-side router can take over
// (the SPA-fallback). The JSON API (/api/*) and /events are registered
// separately and, being more specific, take precedence over the "/" fallback.
func (s *Server) spaRoutes(mux *http.ServeMux) error {
	sub, err := fs.Sub(distFS, "frontend/dist")
	if err != nil {
		return err
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return err // dist not built — `make web-build`
	}
	assets := http.FileServer(http.FS(sub))
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // hashed filenames
		assets.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Real embedded file (e.g. /favicon.ico) → serve it; otherwise the SPA shell.
		if r.URL.Path != "/" {
			if f, ferr := sub.Open(strings.TrimPrefix(r.URL.Path, "/")); ferr == nil {
				_ = f.Close()
				assets.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // shell references hashed assets
		_, _ = w.Write(index)
	})
	return nil
}
