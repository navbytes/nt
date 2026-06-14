package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
			return deriveWorkstream()
		}
		return w // a literal id, or "*" which callers treat as "all"
	}
	if env := strings.TrimSpace(os.Getenv("NT_WORKSTREAM")); env != "" {
		if env == "auto" {
			return deriveWorkstream()
		}
		return env
	}
	return ""
}

// deriveWorkstream infers an identity from the working repo: the checked-out git
// branch (the natural unit of a parallel line of work, and what grove worktrees
// map to), falling back to the working-directory basename for non-git or
// detached-HEAD trees. Returns "" only if even the cwd is unavailable.
func deriveWorkstream() string {
	if b := gitBranch(); b != "" {
		return b
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Base(wd)
	}
	return ""
}

func gitBranch() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	b := strings.TrimSpace(string(out))
	if b == "" || b == "HEAD" { // empty repo or detached HEAD → no branch to name
		return ""
	}
	return b
}

// wsVisible decides whether a task in workstream taskWS is visible to an agent
// currently scoped to `current`. An unscoped agent (current "" ) or an explicit
// widen ("*") sees everything. Otherwise a task is visible when it belongs to
// this workstream OR carries no workstream at all — so the shared human backlog
// (CLI/TUI/web tasks) and pre-workstream tasks stay visible to every agent, and
// only another agent's explicitly-stamped work is hidden.
func wsVisible(taskWS, current string) bool {
	if current == "" || current == "*" {
		return true
	}
	return taskWS == "" || taskWS == current
}
