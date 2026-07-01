// Package workstream resolves the "workstream" identity that isolates one
// parallel line of work from another while sharing a single nt store. Several
// agents (grove worktrees, CI jobs, web/CLI sessions) can run against one store
// at once; each writer's in-flight TASKS carry a todo.txt `ws:` key so reads can
// scope to their own line of work, while NOTES stay shared. Both the CLI and the
// MCP server resolve identity through here so a human's CLI writes and an agent's
// MCP writes agree when NT_WORKSTREAM is set.
package workstream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Env resolves the workstream from the NT_WORKSTREAM environment variable: a
// literal id, or "auto" → derived from the working repo, or "" when unset (no
// scoping — behaves exactly as nt did before workstreams). Isolation is opt-in:
// it activates only once this resolves to a non-empty value.
func Env() string {
	env := strings.TrimSpace(os.Getenv("NT_WORKSTREAM"))
	if env == "" {
		return ""
	}
	if env == "auto" {
		return Derive()
	}
	return env
}

// Derive infers an identity from the working repo: the checked-out git branch
// (the natural unit of a parallel line of work, and what grove worktrees map to),
// falling back to the working-directory basename for non-git or detached-HEAD
// trees. Returns "" only if even the cwd is unavailable.
func Derive() string {
	if b := gitBranch(); b != "" {
		return b
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Base(wd)
	}
	return ""
}

// Visible decides whether a task in workstream taskWS is visible to a reader
// currently scoped to `current`. An unscoped reader (current "") or an explicit
// widen ("*") sees everything. Otherwise a task is visible when it belongs to this
// workstream OR carries no workstream at all — so the shared human backlog and
// pre-workstream tasks stay visible to everyone, and only another agent's
// explicitly-stamped work is hidden.
func Visible(taskWS, current string) bool {
	if current == "" || current == "*" {
		return true
	}
	return taskWS == "" || taskWS == current
}

// Scope resolves the effective current workstream for a read: an explicit value
// (a literal id, or "*" to widen) wins; otherwise the NT_WORKSTREAM environment.
func Scope(explicit string) string {
	if e := strings.TrimSpace(explicit); e != "" {
		if e == "auto" {
			return Derive()
		}
		return e
	}
	return Env()
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
