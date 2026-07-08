package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// opencodeTestEnv points the installer at throwaway config + store dirs and
// returns the OpenCode config dir it will write into.
func opencodeTestEnv(t *testing.T) string {
	t.Helper()
	cfgBase := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgBase)
	t.Setenv("NT_DIR", t.TempDir())
	return filepath.Join(cfgBase, "opencode")
}

func TestOpencodeInstallFull(t *testing.T) {
	cfg := opencodeTestEnv(t)
	if code := cmdOpencode([]string{"install"}); code != 0 {
		t.Fatalf("install exit code = %d", code)
	}

	// 1. MCP registration + 6. skill permission, in one config file.
	root := readJSON(t, filepath.Join(cfg, "opencode.json"))
	mcp, _ := root["mcp"].(map[string]any)
	if _, ok := mcp["nt"].(map[string]any); !ok {
		t.Fatalf("mcp.nt not registered: %v", root["mcp"])
	}
	perm, _ := root["permission"].(map[string]any)
	skill, _ := perm["skill"].(map[string]any)
	if skill["nt"] != "allow" {
		t.Errorf("permission.skill.nt = %v, want allow", skill["nt"])
	}

	// 7. instructions = ["nt-rules.md"] — the hybrid-mode file baseline.
	instr, _ := root["instructions"].([]any)
	if len(instr) != 1 || instr[0] != "nt-rules.md" {
		t.Errorf("instructions = %v, want [\"nt-rules.md\"]", root["instructions"])
	}

	// 2–5. Plugin, skill, commands, AGENTS.md on disk.
	for _, rel := range []string{
		"plugins/nt-memory.ts",
		"skills/nt/SKILL.md",
		"commands/learn.md",
		"commands/recall.md",
		"AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(cfg, filepath.FromSlash(rel))); err != nil {
			t.Errorf("asset not installed: %s (%v)", rel, err)
		}
	}

	// 7. Seeded folders + the initial export.
	e, ok := engine()
	if !ok {
		t.Fatal("open engine")
	}
	var haveRules, haveMemory bool
	for _, n := range mustNotes(e) {
		if strings.HasPrefix(n.Rel, "rules/") {
			haveRules = true
		}
		if strings.HasPrefix(n.Rel, "memory/") {
			haveMemory = true
		}
	}
	if !haveRules || !haveMemory {
		t.Errorf("store not seeded: rules=%v memory=%v", haveRules, haveMemory)
	}
	data, err := os.ReadFile(filepath.Join(cfg, "nt-rules.md"))
	if err != nil {
		t.Fatalf("nt-rules.md not exported: %v", err)
	}
	if !strings.Contains(string(data), "Output style") {
		t.Errorf("nt-rules.md missing the seeded rule: %q", string(data))
	}
}

// A second run must not duplicate seeds, must keep a user-edited AGENTS.md,
// and must refresh a stale (older-version) plugin file.
func TestOpencodeInstallIdempotentAndRefreshes(t *testing.T) {
	cfg := opencodeTestEnv(t)
	if code := cmdOpencode([]string{"install"}); code != 0 {
		t.Fatalf("first install exit code = %d", code)
	}

	agents := filepath.Join(cfg, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(cfg, "plugins", "nt-memory.ts")
	if err := os.WriteFile(plugin, []byte("// stale old version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := cmdOpencode([]string{"install"}); code != 0 {
		t.Fatalf("second install exit code = %d", code)
	}

	if data, _ := os.ReadFile(agents); string(data) != "my own rules\n" {
		t.Errorf("user AGENTS.md was clobbered: %q", string(data))
	}
	if data, _ := os.ReadFile(plugin); strings.Contains(string(data), "stale old version") {
		t.Error("plugin was not refreshed on re-run")
	}

	e, ok := engine()
	if !ok {
		t.Fatal("open engine")
	}
	var rules int
	for _, n := range mustNotes(e) {
		if strings.HasPrefix(n.Rel, "rules/") {
			rules++
		}
	}
	if rules != 1 {
		t.Errorf("rules/ seeded %d times, want exactly 1", rules)
	}
}

// An explicit user choice for permission.skill.nt — even "deny" — is respected,
// and sibling permission keys survive the merge.
func TestEnsureOpencodeSkillPermissionRespectsUserValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{
  "permission": {
    "bash": "ask",
    "skill": {"nt": "deny", "other": "allow"}
  }
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureOpencodeSkillPermission(path)
	if err != nil || changed {
		t.Fatalf("existing value should be left alone: changed=%v err=%v", changed, err)
	}
	root := readJSON(t, path)
	perm := root["permission"].(map[string]any)
	if perm["bash"] != "ask" {
		t.Error("sibling permission key dropped")
	}
	skill := perm["skill"].(map[string]any)
	if skill["nt"] != "deny" || skill["other"] != "allow" {
		t.Errorf("skill permissions altered: %v", skill)
	}
}

// ensureOpencodeInstructions preserves a user's existing instructions list
// (appending, not replacing) and is idempotent — it doesn't duplicate the
// entry on a second call.
func TestEnsureOpencodeInstructionsPreservesExistingEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{"instructions": ["CLAUDE.md"]}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureOpencodeInstructions(path)
	if err != nil || !changed {
		t.Fatalf("expected a change: changed=%v err=%v", changed, err)
	}
	root := readJSON(t, path)
	instr, _ := root["instructions"].([]any)
	if len(instr) != 2 || instr[0] != "CLAUDE.md" || instr[1] != "nt-rules.md" {
		t.Errorf("instructions = %v, want [\"CLAUDE.md\", \"nt-rules.md\"]", root["instructions"])
	}

	// Idempotent: a second call is a no-op, no duplicate entry.
	changed, err = ensureOpencodeInstructions(path)
	if err != nil || changed {
		t.Fatalf("second call should be a no-op: changed=%v err=%v", changed, err)
	}
	root = readJSON(t, path)
	instr, _ = root["instructions"].([]any)
	if len(instr) != 2 {
		t.Errorf("instructions grew on re-run: %v", instr)
	}
}

func TestOpencodeInstallPrintWritesNothing(t *testing.T) {
	cfg := opencodeTestEnv(t)
	if code := cmdOpencode([]string{"install", "--print"}); code != 0 {
		t.Fatalf("print exit code = %d", code)
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Errorf("--print created the config dir: %v", err)
	}
}
