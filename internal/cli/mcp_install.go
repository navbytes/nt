package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
)

// cmdMcpInstall registers `nt mcp` with an AI client.
//
// Claude Code does NOT read MCP servers from ~/.claude/settings.json — they live
// in ~/.claude.json (user scope) or a project .mcp.json, and the supported,
// version-stable way to write them is the `claude` CLI. So for Claude Code we
// shell out to `claude mcp add-json` when that CLI is available, and fall back to
// a direct merge of ~/.claude.json's top-level mcpServers when it isn't. Claude
// Desktop has no CLI, so it's always a direct merge of claude_desktop_config.json.
//
// Either way we use the *absolute* path to this binary (GUI clients launch
// without ~/.local/bin on PATH, so a bare "nt" wouldn't resolve), and the
// operation is idempotent.
//
//	nt mcp install                      # Claude Code (claude CLI, user scope)
//	nt mcp install --client claude-desktop
//	nt mcp install --print              # show what would be done, change nothing
func cmdMcpInstall(args []string) int {
	fs := flag.NewFlagSet("mcp install", flag.ContinueOnError)
	client := fs.String("client", "claude-code", "AI client (claude-code | claude-desktop)")
	print1 := fs.Bool("print", false, "show what would be done, change nothing")
	dryRun := fs.Bool("dry-run", false, "alias for --print")
	flags, _ := splitArgs(args, map[string]bool{"print": true, "dry-run": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	printOnly := *print1 || *dryRun

	bin, err := ntBinaryPath()
	if err != nil {
		return fail(err)
	}
	// The stdio server spec, shared by every client/path.
	entry := map[string]any{"type": "stdio", "command": bin, "args": []any{"mcp"}}

	switch *client {
	case "claude-code", "claude", "code":
		return installClaudeCode(bin, entry, printOnly)
	case "claude-desktop", "desktop":
		path, err := desktopConfigPath()
		if err != nil {
			return fail(err)
		}
		return installFileMerge(path, "Claude Desktop", entry, printOnly)
	default:
		return usageErr(fmt.Errorf("unknown client %q (supported: claude-code, claude-desktop; or use --print)", *client))
	}
}

// ntBinaryPath returns the absolute, symlink-resolved path to this executable.
func ntBinaryPath() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate nt binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	return bin, nil
}

// installClaudeCode registers with Claude Code, preferring the `claude` CLI
// (which writes the correct file for the chosen scope) and falling back to a
// direct merge of ~/.claude.json when the CLI isn't on PATH.
func installClaudeCode(bin string, entry map[string]any, printOnly bool) int {
	spec, _ := json.Marshal(entry)

	if printOnly {
		fmt.Println("With the Claude Code CLI:")
		fmt.Printf("  claude mcp add-json nt '%s' --scope user\n\n", spec)
		fmt.Println("Or add this to ~/.claude.json under a top-level \"mcpServers\":")
		out, _ := json.MarshalIndent(map[string]any{"nt": entry}, "  ", "  ")
		fmt.Printf("  %s\n", out)
		return 0
	}

	if claude, err := exec.LookPath("claude"); err == nil {
		// Idempotent upsert: remove any stale entry first (ignore its result),
		// then add the current one.
		_ = exec.Command(claude, "mcp", "remove", "nt", "--scope", "user").Run() //nolint:errcheck
		cmd := exec.Command(claude, "mcp", "add-json", "nt", string(spec), "--scope", "user")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fail(fmt.Errorf("claude mcp add-json: %v: %s", err, out))
		}
		fmt.Printf("registered nt with Claude Code (user scope) via the claude CLI\n  command: %s mcp\n", bin)
		fmt.Println("open a new Claude Code session (or run /mcp) to pick it up.")
		return 0
	}

	// Fallback: no claude CLI — merge the correct file directly.
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(fmt.Errorf("locate home dir: %w", err))
	}
	path := filepath.Join(home, ".claude.json")
	fmt.Println("note: the `claude` CLI wasn't found on PATH; editing ~/.claude.json directly.")
	return installFileMerge(path, "Claude Code", entry, false)
}

// installFileMerge writes mcpServers.nt into a JSON config file, preserving every
// other key, and reports what changed.
func installFileMerge(path, label string, entry map[string]any, printOnly bool) int {
	if printOnly {
		snippet := map[string]any{"mcpServers": map[string]any{"nt": entry}}
		out, _ := json.MarshalIndent(snippet, "", "  ")
		fmt.Printf("Add to %s (%s):\n%s\n", path, label, out)
		return 0
	}
	changed, prev, err := mergeMCPEntry(path, "nt", entry)
	if err != nil {
		return fail(err)
	}
	bin, _ := entry["command"].(string)
	switch {
	case !changed && prev != nil:
		fmt.Printf("nt is already registered in %s (no change)\n", path)
	case prev != nil:
		fmt.Printf("updated nt registration in %s\n  command: %s mcp\n", path, bin)
	default:
		fmt.Printf("registered nt in %s (%s)\n  command: %s mcp\n", path, label, bin)
	}
	if changed {
		fmt.Println("restart the client (or reload its MCP servers) to pick it up.")
	}
	return 0
}

// desktopConfigPath resolves Claude Desktop's config file for the current OS.
func desktopConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "Claude", "claude_desktop_config.json"), nil
	default:
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "Claude", "claude_desktop_config.json"), nil
	}
}

// mergeMCPEntry sets mcpServers.<name> = entry in the JSON config at path,
// preserving every other key. It creates the file (and parent dirs) when
// missing. Returns whether the file was changed and the previous entry (nil if
// none existed). The write is atomic (temp + rename in the same dir).
func mergeMCPEntry(path, name string, entry map[string]any) (changed bool, prev map[string]any, err error) {
	root := map[string]any{}
	if data, rerr := os.ReadFile(path); rerr == nil {
		if len(data) > 0 {
			if jerr := json.Unmarshal(data, &root); jerr != nil {
				return false, nil, fmt.Errorf("%s is not valid JSON (%v); fix it or use --print and edit by hand", path, jerr)
			}
		}
	} else if !os.IsNotExist(rerr) {
		return false, nil, fmt.Errorf("read %s: %w", path, rerr)
	}

	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		if _, exists := root["mcpServers"]; exists {
			return false, nil, fmt.Errorf("%s has a non-object \"mcpServers\" value; fix it or use --print", path)
		}
		servers = map[string]any{}
	}
	if existing, ok := servers[name].(map[string]any); ok {
		prev = existing
	}
	if prev != nil && reflect.DeepEqual(prev, entry) {
		return false, prev, nil // already correct — idempotent no-op
	}

	servers[name] = entry
	root["mcpServers"] = servers

	out, merr := json.MarshalIndent(root, "", "  ")
	if merr != nil {
		return false, prev, fmt.Errorf("encode config: %w", merr)
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, prev, fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nt-mcp-*.tmp")
	if err != nil {
		return false, prev, fmt.Errorf("write config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return false, prev, fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, prev, fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, prev, fmt.Errorf("install config: %w", err)
	}
	return true, prev, nil
}
