package mcp

import (
	"strings"

	"github.com/navbytes/nt/internal/workstream"
)

// Workstreams isolate one parallel line of work from another while sharing the
// same nt store. Several agents (grove worktrees, CI jobs, web sessions) can run
// against one store at once; each agent's in-flight TASKS should stay its own,
// but NOTES — the knowledge base — stay shared so findings cross-pollinate.
//
// A task carries its workstream in the todo.txt `ws:` key. Reads scope to the
// current workstream; writes stamp it. Notes are never scoped by workstream.
//
// Identity is resolved generically so this works beyond any one tool, first hit
// wins:
//
//  1. an explicit `workstream` call arg — manual override / tests; "*" widens a
//     read to every workstream; "auto" forces derivation (step 3).
//  2. the NT_WORKSTREAM env var — what grove / CI / a session harness exports as
//     its identity. The literal value "auto" means "derive it" (step 3).
//  3. derivation from the working repo — the current git branch, else the
//     working-directory basename. Reached only via the "auto" sentinel above.
//  4. nothing → "" → no scoping: behaves exactly as nt did before workstreams,
//     so a solo user who sets none of this sees no change.
//
// Isolation is therefore opt-in: it activates only once an identity resolves to
// a non-empty value.
func (s *server) workstream(a map[string]any) string {
	if w := strings.TrimSpace(str(a, "workstream")); w != "" {
		if w == "auto" {
			return workstream.Derive()
		}
		return w // a literal id, or "*" which callers treat as "all"
	}
	return workstream.Env()
}
