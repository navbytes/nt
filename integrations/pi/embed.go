// Package pi embeds the nt ↔ Pi integration bundle — the nt-memory extension
// (which bridges nt's MCP server into Pi's native tools and injects rules +
// core memory), the nt skill, the /learn and /recall prompt templates, and the
// starter AGENTS.md — so `nt pi install` can set up a complete integration from
// any installed binary, with no repo checkout.
// install.sh in this directory is the repo-checkout equivalent of the same
// steps; keep the two file lists in sync.
package pi

import "embed"

// Assets holds the installable integration files, addressed by their path
// relative to this directory (e.g. "extensions/nt-memory.ts").
//
//go:embed extensions skills prompts AGENTS.md
var Assets embed.FS
