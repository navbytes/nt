# nt — developer tasks. Run `make` (or `make help`) to see every command.

BINARY         := nt
WEB_DIR        := internal/web/frontend
NT_INSTALL_DIR ?= $(HOME)/.local/bin              # override: make install NT_INSTALL_DIR=/path
VERSION        := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS        := -s -w -X main.version=$(VERSION)
TYGO           := github.com/gzuidhof/tygo@v0.2.21

.DEFAULT_GOAL := help

##@ Build & install

build: ## Build the nt CLI binary to ./nt
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: build ## Build, then install nt to ~/.local/bin (override with NT_INSTALL_DIR)
	@mkdir -p $(NT_INSTALL_DIR)
	@install -m 0755 $(BINARY) $(NT_INSTALL_DIR)/$(BINARY)
	@echo "installed $(BINARY) $(VERSION) -> $(NT_INSTALL_DIR)/$(BINARY)"
	@case ":$$PATH:" in *":$(NT_INSTALL_DIR):"*) ;; *) echo "note: add $(NT_INSTALL_DIR) to your PATH";; esac

uninstall: ## Remove the installed nt binary
	@rm -f $(NT_INSTALL_DIR)/$(BINARY) && echo "removed $(NT_INSTALL_DIR)/$(BINARY)"

clean: ## Delete the built ./nt binary
	rm -f $(BINARY)

##@ Go quality

test: ## Run the Go test suite
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go code in place (gofmt -w)
	gofmt -w .

##@ Web frontend (Svelte SPA in internal/web/frontend)
# The built bundle is committed to internal/web/frontend/dist and embedded into
# the Go binary, so `go build` / `go install` (which never run npm) still ship a
# current UI. After changing the frontend, run `make web-build` and commit the
# regenerated dist/ — CI fails if the committed dist isn't a fresh build.

web-build: ## Reinstall deps + rebuild the committed SPA bundle (dist/)
	cd $(WEB_DIR) && npm ci && npm run build

web-dev: ## Start the Vite dev server (HMR, :5173) against a running `nt web`
	cd $(WEB_DIR) && npm install && npm run dev

web-check: ## Type-check the SPA (svelte-check)
	cd $(WEB_DIR) && npm run check

web-types: ## Regenerate the SPA's TS types from the Go wire contract (commit the result)
	go run $(TYGO) generate

##@ Run the local web UI (over your real store, $NT_DIR)

web: build ## Serve the web UI in the background, editing on (stop with: make web-stop)
	./$(BINARY) web -detach

web-stop: build ## Stop the backgrounded web UI
	./$(BINARY) web --stop

##@ Desktop app (Wails — a separate nested module under desktop/)
# desktop/ keeps its Wails/CGO/WebKit deps out of the CLI's go.mod, so
# `go install github.com/navbytes/nt@latest` never builds it. `-tags production`
# selects Wails' native webview (a bare `go run .` compiles a stub that exits).

desktop: ## Run the native desktop window over your real store ($NT_DIR)
	cd desktop && go run -tags production .

desktop-build: ## Compile the desktop binary to desktop/nt-desktop
	cd desktop && go build -tags production -o nt-desktop . && echo "built desktop/nt-desktop"

##@ Release (goreleaser)

snapshot: ## Build release artifacts locally, without publishing
	goreleaser release --snapshot --clean

release: ## Cut a real release — needs goreleaser + a pushed git tag
	goreleaser release --clean

##@ Misc

render: ## Regenerate docs/tui-render.html (real TUI frames, for design review)
	NT_RENDER_HTML=1 go test ./internal/tui/ -run TestRenderHTML

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next} \
		/^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build install uninstall clean test vet fmt \
	web-build web-dev web-check web-types web web-stop \
	desktop desktop-build snapshot release render help
