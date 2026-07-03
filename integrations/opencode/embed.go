// Package opencode embeds the nt ↔ OpenCode integration bundle — the
// nt-memory plugin, the nt skill, the /learn and /recall commands, and the
// starter AGENTS.md — so `nt opencode install` can set up a complete
// integration from any installed binary, with no repo checkout.
// install.sh in this directory is the repo-checkout equivalent of the same
// steps; keep the two file lists in sync.
package opencode

import "embed"

// Assets holds the installable integration files, addressed by their path
// relative to this directory (e.g. "plugins/nt-memory.ts").
//
//go:embed plugins skills commands AGENTS.md
var Assets embed.FS
