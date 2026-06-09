BINARY         := nt
VERSION        := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
NT_INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_DIR    := $(NT_INSTALL_DIR)
LDFLAGS        := -s -w -X main.version=$(VERSION)

.PHONY: build install uninstall test vet fmt render clean release snapshot desktop desktop-build web web-stop web-build web-dev web-check web-types

TYGO := github.com/gzuidhof/tygo@v0.2.21

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# Start the web UI in the background with editing enabled, over your real store
# ($NT_DIR). Equivalent to `nt web --edit --detach`; stop it with `make web-stop`.
web: build
	./$(BINARY) web --edit --detach

# Stop the backgrounded web UI (`nt web --stop`).
web-stop: build
	./$(BINARY) web --stop

# --- Svelte SPA (internal/web/frontend) --------------------------------------
# The built bundle is committed (internal/web/frontend/dist) so `go build` /
# `go install` — which never run npm — still embed a current UI. Run web-build
# after changing the frontend, then commit the regenerated dist/. CI verifies
# the committed dist matches a fresh build.
WEB_DIR := internal/web/frontend

web-build:
	cd $(WEB_DIR) && npm ci && npm run build

# Dev: vite (HMR) on :5173 proxying the API to a running `nt web`. In another
# terminal: `nt web --edit --port 8765` (or set NT_API).
web-dev:
	cd $(WEB_DIR) && npm install && npm run dev

web-check:
	cd $(WEB_DIR) && npm run check

# Regenerate the SPA's TypeScript types from the Go wire contract (apitypes),
# then commit the result. CI fails if this output is stale.
web-types:
	go run $(TYGO) generate

install: build
	@mkdir -p $(INSTALL_DIR)
	@install -m 0755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "installed $(BINARY) $(VERSION) -> $(INSTALL_DIR)/$(BINARY)"
	@case ":$$PATH:" in *":$(INSTALL_DIR):"*) ;; *) echo "note: add $(INSTALL_DIR) to your PATH";; esac

uninstall:
	@rm -f $(INSTALL_DIR)/$(BINARY) && echo "removed $(INSTALL_DIR)/$(BINARY)"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Regenerate docs/tui-render.html (real TUI frames) for design review.
render:
	NT_RENDER_HTML=1 go test ./internal/tui/ -run TestRenderHTML

clean:
	rm -f $(BINARY)

# Cut a real release (needs goreleaser + a pushed git tag).
release:
	goreleaser release --clean

# Build local release artifacts without publishing.
snapshot:
	goreleaser release --snapshot --clean

# --- Wails desktop spike -----------------------------------------------------
# desktop/ is a SEPARATE nested module: its Wails/CGO/WebKit deps never enter
# the CLI's go.mod, and `go install github.com/navbytes/nt@latest` never builds
# it. These targets opt in explicitly. `-tags production` selects Wails' native
# webview (a bare `go run .` compiles a build-tag stub that exits).

# Run the native window over your real store ($NT_DIR).
desktop:
	cd desktop && go run -tags production .

# Compile the desktop binary to desktop/nt-desktop (gitignored).
desktop-build:
	cd desktop && go build -tags production -o nt-desktop . && echo "built desktop/nt-desktop"
