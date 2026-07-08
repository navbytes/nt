package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// integrationsTestEnv isolates HOME, XDG_CONFIG_HOME, PI_CODING_AGENT_DIR, and
// NT_DIR so `nt doctor --integrations` never sees this machine's real config.
func integrationsTestEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	t.Setenv("NT_DIR", t.TempDir())
	return home
}

func TestDoctorIntegrationsNoneDetected(t *testing.T) {
	integrationsTestEnv(t)
	out, code := runWithStdout("doctor", "--integrations")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, out)
	}
	if !strings.Contains(out, "no nt integrations detected") {
		t.Errorf("expected a clean-skip message, got:\n%s", out)
	}
}

func TestDoctorIntegrationsOpenCodeHealthyAfterInstall(t *testing.T) {
	integrationsTestEnv(t)
	captureRun(t, "opencode", "install")
	out, code := runWithStdout("doctor", "--integrations")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 after a fresh install: %s", code, out)
	}
	if !strings.Contains(out, "OpenCode: healthy") {
		t.Errorf("expected OpenCode to report healthy:\n%s", out)
	}
}

func TestDoctorIntegrationsOpenCodeDetectsAssetDrift(t *testing.T) {
	home := integrationsTestEnv(t)
	captureRun(t, "opencode", "install")
	pluginPath := filepath.Join(home, ".config", "opencode", "plugins", "nt-memory.ts")
	if err := os.WriteFile(pluginPath, []byte("// stale content from an older nt version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runWithStdout("doctor", "--integrations")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 with drifted asset: %s", code, out)
	}
	if !strings.Contains(out, "plugins/nt-memory.ts") || !strings.Contains(out, "out of date") {
		t.Errorf("expected an out-of-date finding for the plugin:\n%s", out)
	}
}

func TestDoctorIntegrationsOpenCodeDetectsMissingMCPEntry(t *testing.T) {
	home := integrationsTestEnv(t)
	captureRun(t, "opencode", "install")
	cfgPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	delete(root, "mcp")
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	code := runIntegrationsDoctor()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 with mcp.nt missing", code)
	}
}

func TestDoctorIntegrationsPiHealthyAfterInstall(t *testing.T) {
	integrationsTestEnv(t)
	captureRun(t, "pi", "install")
	out, code := runWithStdout("doctor", "--integrations")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 after a fresh install: %s", code, out)
	}
	if !strings.Contains(out, "Pi: healthy") {
		t.Errorf("expected Pi to report healthy:\n%s", out)
	}
}

func TestDoctorIntegrationsPiDetectsAssetDrift(t *testing.T) {
	home := integrationsTestEnv(t)
	captureRun(t, "pi", "install")
	skillPath := filepath.Join(home, ".pi", "agent", "skills", "nt", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("# stale skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runWithStdout("doctor", "--integrations")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 with drifted asset: %s", code, out)
	}
	if !strings.Contains(out, "skills/nt/SKILL.md") || !strings.Contains(out, "out of date") {
		t.Errorf("expected an out-of-date finding for the skill:\n%s", out)
	}
}

func TestDoctorIntegrationsClaudeCodeHookMatchers(t *testing.T) {
	home := integrationsTestEnv(t)
	settingsDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only the TodoWrite matcher wired — Bash is missing.
	settings := `{"hooks":{"PostToolUse":[{"matcher":"TodoWrite","hooks":[{"type":"command","command":"nt hook"}]}]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runWithStdout("doctor", "--integrations")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 with the Bash matcher missing: %s", code, out)
	}
	if !strings.Contains(out, "Bash matcher is missing") {
		t.Errorf("expected a missing-Bash-matcher finding:\n%s", out)
	}

	// Add the Bash matcher too — should now be healthy.
	settings = `{"hooks":{"PostToolUse":[
		{"matcher":"TodoWrite","hooks":[{"type":"command","command":"nt hook"}]},
		{"matcher":"Bash","hooks":[{"type":"command","command":"nt hook"}]}
	]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = runWithStdout("doctor", "--integrations")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 with both matchers wired: %s", code, out)
	}
	if !strings.Contains(out, "Claude Code: healthy") {
		t.Errorf("expected Claude Code to report healthy:\n%s", out)
	}
}

func TestDoctorIntegrationsClaudeCodeMissingBinary(t *testing.T) {
	home := integrationsTestEnv(t)
	claudeJSON := filepath.Join(home, ".claude.json")
	cfg := `{"mcpServers":{"nt":{"type":"stdio","command":"/no/such/nt-binary","args":["mcp"]}}}`
	if err := os.WriteFile(claudeJSON, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runWithStdout("doctor", "--integrations")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 with a missing binary path: %s", code, out)
	}
	if !strings.Contains(out, "missing binary") {
		t.Errorf("expected a missing-binary finding:\n%s", out)
	}
}

// The whole point of --integrations is that it's read-only — confirm it
// never touches any of the config files it inspects.
func TestDoctorIntegrationsNeverWrites(t *testing.T) {
	home := integrationsTestEnv(t)
	captureRun(t, "opencode", "install")
	captureRun(t, "pi", "install")

	before := map[string][]byte{}
	paths := []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".pi", "agent", "extensions", "nt-memory.ts"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = data
	}

	captureRun(t, "doctor", "--integrations")

	for _, p := range paths {
		after, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before[p]) {
			t.Errorf("doctor --integrations modified %s", p)
		}
	}
}
