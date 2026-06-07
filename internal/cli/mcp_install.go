package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
)

// cmdMcpInstall registers `nt mcp` with an AI client by merging an mcpServers.nt
// entry into the client's config, using the *absolute* path to this binary (GUI
// clients often launch without ~/.local/bin on PATH, so a bare "nt" wouldn't
// resolve). It is idempotent: re-running is a no-op when the entry already
// matches, and updates in place when the binary has moved.
//
//	nt mcp install                      # Claude Code (~/.claude/settings.json)
//	nt mcp install --client claude-desktop
//	nt mcp install --print              # show the snippet, write nothing
func cmdMcpInstall(args []string) int {
	client := "claude-code"
	printOnly := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--client", "-c":
			if i+1 >= len(args) {
				return fail(fmt.Errorf("--client needs a value (claude-code | claude-desktop)"))
			}
			i++
			client = args[i]
		case "--print", "--dry-run", "-n":
			printOnly = true
		default:
			return fail(fmt.Errorf("unknown flag %q (try: nt mcp install [--client claude-code|claude-desktop] [--print])", args[i]))
		}
	}

	bin, err := os.Executable()
	if err != nil {
		return fail(fmt.Errorf("locate nt binary: %w", err))
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}

	// The entry every client uses (Claude Code and Desktop share the schema).
	entry := map[string]any{"command": bin, "args": []any{"mcp"}}

	if printOnly {
		snippet := map[string]any{"mcpServers": map[string]any{"nt": entry}}
		out, _ := json.MarshalIndent(snippet, "", "  ")
		fmt.Println(string(out))
		return 0
	}

	cfgPath, err := clientConfigPath(client)
	if err != nil {
		return fail(err)
	}

	changed, prev, err := mergeMCPEntry(cfgPath, "nt", entry)
	if err != nil {
		return fail(err)
	}

	switch {
	case !changed && prev != nil:
		fmt.Printf("nt is already registered in %s (no change)\n", cfgPath)
	case prev != nil:
		fmt.Printf("updated nt registration in %s\n  command: %s mcp\n", cfgPath, bin)
	default:
		fmt.Printf("registered nt in %s\n  command: %s mcp\n", cfgPath, bin)
	}
	if changed {
		fmt.Println("restart the client (or reload its MCP servers) to pick it up.")
	}
	return 0
}

// clientConfigPath resolves the settings file for a known AI client.
func clientConfigPath(client string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	switch client {
	case "claude-code", "claude", "code":
		return filepath.Join(home, ".claude", "settings.json"), nil
	case "claude-desktop", "desktop":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
		}
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "Claude", "claude_desktop_config.json"), nil
	default:
		return "", fmt.Errorf("unknown client %q (supported: claude-code, claude-desktop; or use --print for others)", client)
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
