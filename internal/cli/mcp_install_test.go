package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func ntEntry(bin string) map[string]any {
	return map[string]any{"command": bin, "args": []any{"mcp"}}
}

func TestMergeMCPEntryCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	changed, prev, err := mergeMCPEntry(path, "nt", ntEntry("/abs/nt"))
	if err != nil || !changed || prev != nil {
		t.Fatalf("fresh install: changed=%v prev=%v err=%v", changed, prev, err)
	}
	got := readJSON(t, path)
	servers := got["mcpServers"].(map[string]any)
	if !reflect.DeepEqual(servers["nt"], ntEntry("/abs/nt")) {
		t.Fatalf("entry not written: %v", servers["nt"])
	}
}

func TestMergeMCPEntryIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, _, err := mergeMCPEntry(path, "nt", ntEntry("/abs/nt")); err != nil {
		t.Fatal(err)
	}
	changed, prev, err := mergeMCPEntry(path, "nt", ntEntry("/abs/nt"))
	if err != nil || changed || prev == nil {
		t.Fatalf("second run should be a no-op: changed=%v prev=%v err=%v", changed, prev, err)
	}
}

func TestMergeMCPEntryUpdatesMovedBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, _, err := mergeMCPEntry(path, "nt", ntEntry("/old/nt")); err != nil {
		t.Fatal(err)
	}
	changed, prev, err := mergeMCPEntry(path, "nt", ntEntry("/new/nt"))
	if err != nil || !changed || prev == nil {
		t.Fatalf("moved binary should update: changed=%v prev=%v err=%v", changed, prev, err)
	}
	got := readJSON(t, path)["mcpServers"].(map[string]any)["nt"].(map[string]any)
	if got["command"] != "/new/nt" {
		t.Fatalf("command not updated: %v", got["command"])
	}
}

func TestMergeMCPEntryPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{
  "hooks": {"PostToolUse": [{"matcher": "TodoWrite"}]},
  "mcpServers": {"other": {"command": "x"}}
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mergeMCPEntry(path, "nt", ntEntry("/abs/nt")); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, path)
	if _, ok := got["hooks"]; !ok {
		t.Error("hooks key was dropped")
	}
	servers := got["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("sibling mcp server was dropped")
	}
	if !reflect.DeepEqual(servers["nt"], ntEntry("/abs/nt")) {
		t.Errorf("nt entry wrong: %v", servers["nt"])
	}
}

func TestMergeMCPEntryRejectsBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mergeMCPEntry(path, "nt", ntEntry("/abs/nt")); err == nil {
		t.Fatal("expected an error on invalid JSON, got nil")
	}
}

// OpenCode nests servers under "mcp" (not "mcpServers") and a fresh file should
// get a "$schema" stamp so editors validate it.
func TestMergeNamedEntryOpencodeFreshStampsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	entry := opencodeEntry("/abs/nt")
	changed, prev, err := mergeNamedEntry(path, "mcp", "nt", entry, opencodeSchema)
	if err != nil || !changed || prev != nil {
		t.Fatalf("fresh install: changed=%v prev=%v err=%v", changed, prev, err)
	}
	got := readJSON(t, path)
	if got["$schema"] != opencodeSchema {
		t.Errorf("$schema not stamped on fresh file: %v", got["$schema"])
	}
	if _, ok := got["mcpServers"]; ok {
		t.Error("should not write Claude's mcpServers key for OpenCode")
	}
	mcp := got["mcp"].(map[string]any)
	if !reflect.DeepEqual(mcp["nt"], opencodeEntry("/abs/nt")) {
		t.Fatalf("entry not written under mcp.nt: %v", mcp["nt"])
	}
}

// opencodeEntry is the "local" stdio shape OpenCode expects: command is an argv
// array, not Claude's command-string + args.
func TestOpencodeEntryShape(t *testing.T) {
	e := opencodeEntry("/abs/nt")
	if e["type"] != "local" || e["enabled"] != true {
		t.Fatalf("unexpected type/enabled: %v", e)
	}
	cmd, ok := e["command"].([]any)
	if !ok || len(cmd) != 2 || cmd[0] != "/abs/nt" || cmd[1] != "mcp" {
		t.Fatalf("command should be argv array [bin mcp]: %v", e["command"])
	}
}

// An existing opencode.json that already has nt is a no-op, and a user-set
// "$schema" or sibling keys are preserved (no re-stamp on existing files).
func TestMergeNamedEntryOpencodePreservesAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-opus-4-8",
  "mcp": {"other": {"type": "local", "command": ["x"]}}
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mergeNamedEntry(path, "mcp", "nt", opencodeEntry("/abs/nt"), opencodeSchema); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, path)
	if got["model"] != "anthropic/claude-opus-4-8" {
		t.Error("sibling top-level key was dropped")
	}
	mcp := got["mcp"].(map[string]any)
	if _, ok := mcp["other"]; !ok {
		t.Error("sibling mcp server was dropped")
	}
	changed, prev, err := mergeNamedEntry(path, "mcp", "nt", opencodeEntry("/abs/nt"), opencodeSchema)
	if err != nil || changed || prev == nil {
		t.Fatalf("second run should be a no-op: changed=%v prev=%v err=%v", changed, prev, err)
	}
}
