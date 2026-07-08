package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	opencodeassets "github.com/navbytes/nt/integrations/opencode"
	piassets "github.com/navbytes/nt/integrations/pi"
)

// integrationReport is one client's `nt doctor --integrations` result.
// Installed distinguishes "checked and found nothing wrong" from "no trace
// of this client — nothing to check": a client the user never set up must
// never be reported as broken.
type integrationReport struct {
	Client    string
	Installed bool
	Findings  []string
}

// runIntegrationsDoctor is the read-only counterpart of `nt doctor`'s
// task-file reconciliation: it can't observe another process's in-memory
// state (a dead Pi bridge, an OpenCode build silently discarding
// system.transform), so it's scoped to what nt's own install code already
// has the oracle for — config wiring, asset drift against the embedded
// bundle, and stale absolute binary paths. Never writes.
func runIntegrationsDoctor() int {
	reports := []integrationReport{checkClaudeCodeIntegration(), checkOpenCodeIntegration(), checkPiIntegration()}
	seen, problems := false, false
	for _, r := range reports {
		if !r.Installed {
			continue
		}
		seen = true
		if len(r.Findings) == 0 {
			fmt.Printf("%s: healthy\n", r.Client)
			continue
		}
		problems = true
		fmt.Printf("%s:\n", r.Client)
		for _, f := range r.Findings {
			fmt.Printf("  ⚠ %s\n", f)
		}
	}
	if !seen {
		fmt.Println("no nt integrations detected (Claude Code, OpenCode, Pi) — nothing to check")
		return 0
	}
	return map[bool]int{true: 1, false: 0}[problems]
}

// checkClaudeCodeIntegration checks the MCP registration (~/.claude.json)
// and the two PostToolUse hook matchers (docs/claude-integration.md §1).
// Claude Code's own `claude` CLI may store the MCP entry somewhere this
// check can't see, so a missing entry is reported as inconclusive, not a
// hard failure, when the CLI is present.
func checkClaudeCodeIntegration() integrationReport {
	r := integrationReport{Client: "Claude Code"}
	home, err := os.UserHomeDir()
	if err != nil {
		return r
	}

	// Hook and MCP are independent, separately-optional integration points
	// (docs/claude-integration.md's §1 vs §3) — a user who only wired the
	// hook, or only registered the MCP server, made a deliberate choice, not
	// a mistake. So an absent mcpServers.nt entry is never a finding here;
	// only STALENESS of one that already exists is.
	cfgPath := filepath.Join(home, ".claude.json")
	if root, fresh, cerr := readConfigMap(cfgPath); cerr != nil {
		r.Installed = true
		r.Findings = append(r.Findings, fmt.Sprintf("%s: %v", cfgPath, cerr))
	} else if !fresh {
		if servers, ok := root["mcpServers"].(map[string]any); ok {
			if entry, ok := servers["nt"].(map[string]any); ok {
				r.Installed = true
				if cmd, ok := entry["command"].(string); ok && cmd != "" {
					if _, serr := os.Stat(cmd); serr != nil {
						r.Findings = append(r.Findings, fmt.Sprintf("MCP entry points at a missing binary: %s (re-run `nt mcp install`)", cmd))
					}
				}
			}
		}
	}

	settingsPaths := []string{filepath.Join(home, ".claude", "settings.json")}
	if cwd, cerr := os.Getwd(); cerr == nil {
		settingsPaths = append(settingsPaths,
			filepath.Join(cwd, ".claude", "settings.json"),
			filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	haveTodo, haveBash, sawSettings := false, false, false
	for _, p := range settingsPaths {
		root, fresh, cerr := readConfigMap(p)
		if cerr != nil || fresh {
			continue
		}
		sawSettings = true
		hooks, _ := root["hooks"].(map[string]any)
		post, _ := hooks["PostToolUse"].([]any)
		for _, raw := range post {
			m, _ := raw.(map[string]any)
			matcher, _ := m["matcher"].(string)
			if matcher != "TodoWrite" && matcher != "Bash" {
				continue
			}
			hs, _ := m["hooks"].([]any)
			for _, hraw := range hs {
				h, _ := hraw.(map[string]any)
				cmdStr, _ := h["command"].(string)
				if strings.Contains(cmdStr, "nt hook") {
					if matcher == "TodoWrite" {
						haveTodo = true
					} else {
						haveBash = true
					}
				}
			}
		}
	}
	if sawSettings {
		r.Installed = true
		switch {
		case haveTodo && haveBash:
			// fully wired
		case !haveTodo && !haveBash:
			r.Findings = append(r.Findings, "no `nt hook` wired in PostToolUse (see docs/claude-integration.md §1)")
		case !haveTodo:
			r.Findings = append(r.Findings, "`nt hook`'s TodoWrite matcher is missing — todo mirroring won't run")
		case !haveBash:
			r.Findings = append(r.Findings, "`nt hook`'s Bash matcher is missing — error-triggered lesson recall won't run")
		}
	}

	return r
}

// checkOpenCodeIntegration checks opencode.json's mcp/permission/instructions
// wiring plus byte-for-byte drift of the installed plugin/skill/commands
// against the versions embedded in this binary.
func checkOpenCodeIntegration() integrationReport {
	r := integrationReport{Client: "OpenCode"}
	cfgPath, err := opencodeConfigPath()
	if err != nil {
		return r
	}
	root, fresh, err := readConfigMap(cfgPath)
	if err != nil {
		r.Installed = true
		r.Findings = append(r.Findings, fmt.Sprintf("%s: %v", cfgPath, err))
		return r
	}
	if fresh {
		return r // no config at all — OpenCode integration not installed
	}
	r.Installed = true
	cfgDir := filepath.Dir(cfgPath)

	mcpEntry, _ := root["mcp"].(map[string]any)
	nt, ntOK := mcpEntry["nt"].(map[string]any)
	switch {
	case !ntOK:
		r.Findings = append(r.Findings, "mcp.nt not registered (re-run `nt opencode install`)")
	default:
		if cmdArr, ok := nt["command"].([]any); ok && len(cmdArr) > 0 {
			if binPath, ok := cmdArr[0].(string); ok && binPath != "" {
				if _, serr := os.Stat(binPath); serr != nil {
					r.Findings = append(r.Findings, fmt.Sprintf("mcp.nt points at a missing binary: %s (re-run `nt opencode install`)", binPath))
				}
			}
		}
	}

	perm, _ := root["permission"].(map[string]any)
	skill, _ := perm["skill"].(map[string]any)
	if v, ok := skill["nt"].(string); !ok || v == "" {
		r.Findings = append(r.Findings, "permission.skill.nt not set — the skill will prompt every time (re-run `nt opencode install`)")
	}

	instrOK := false
	if list, ok := root["instructions"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s == "nt-rules.md" {
				instrOK = true
			}
		}
	}
	if !instrOK {
		r.Findings = append(r.Findings, "\"nt-rules.md\" missing from instructions — the guaranteed file baseline won't load (re-run `nt opencode install`)")
	}

	for _, a := range opencodeAssets {
		embedded, rerr := opencodeassets.Assets.ReadFile(a.src)
		if rerr != nil {
			continue
		}
		dst := filepath.Join(cfgDir, a.dst)
		onDisk, derr := os.ReadFile(dst)
		switch {
		case derr != nil:
			r.Findings = append(r.Findings, fmt.Sprintf("missing %s (re-run `nt opencode install`)", a.dst))
		case !bytes.Equal(embedded, onDisk):
			r.Findings = append(r.Findings, fmt.Sprintf("%s is out of date vs this nt binary (re-run `nt opencode install`)", a.dst))
		}
	}
	return r
}

// checkPiIntegration checks byte-for-byte drift of the installed
// extension/skill/prompts against the versions embedded in this binary. Pi
// has no central JSON config (SPEC/pi_install.go), so there's no wiring to
// check beyond the files existing and matching.
func checkPiIntegration() integrationReport {
	r := integrationReport{Client: "Pi"}
	cfgDir, err := piConfigDir()
	if err != nil {
		return r
	}
	extPath := filepath.Join(cfgDir, "extensions", "nt-memory.ts")
	if _, serr := os.Stat(extPath); serr != nil {
		return r // not installed
	}
	r.Installed = true
	for _, a := range piAssets {
		embedded, rerr := piassets.Assets.ReadFile(a.src)
		if rerr != nil {
			continue
		}
		dst := filepath.Join(cfgDir, a.dst)
		onDisk, derr := os.ReadFile(dst)
		switch {
		case derr != nil:
			r.Findings = append(r.Findings, fmt.Sprintf("missing %s (re-run `nt pi install`)", a.dst))
		case !bytes.Equal(embedded, onDisk):
			r.Findings = append(r.Findings, fmt.Sprintf("%s is out of date vs this nt binary (re-run `nt pi install`)", a.dst))
		}
	}
	return r
}
