package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// piTestEnv points the installer at a throwaway Pi config dir + store dir and
// returns the Pi config dir it will write into.
func piTestEnv(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "agent")
	t.Setenv("PI_CODING_AGENT_DIR", cfg)
	t.Setenv("NT_DIR", t.TempDir())
	return cfg
}

func TestPiInstallFull(t *testing.T) {
	cfg := piTestEnv(t)
	if code := cmdPi([]string{"install"}); code != 0 {
		t.Fatalf("install exit code = %d", code)
	}

	// 1–4. Extension, skill, prompts, AGENTS.md on disk.
	for _, rel := range []string{
		"extensions/nt-memory.ts",
		"skills/nt/SKILL.md",
		"prompts/learn.md",
		"prompts/recall.md",
		"AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(cfg, filepath.FromSlash(rel))); err != nil {
			t.Errorf("asset not installed: %s (%v)", rel, err)
		}
	}

	// The extension should be the bridge, not an MCP-client registration (Pi has
	// no MCP). Sanity-check that the shipped extension mentions the bridge.
	ext, err := os.ReadFile(filepath.Join(cfg, "extensions", "nt-memory.ts"))
	if err != nil {
		t.Fatalf("read extension: %v", err)
	}
	if !strings.Contains(string(ext), "registerTool") {
		t.Errorf("extension does not register tools: %q", string(ext)[:200])
	}

	// 5. Seeded folders + the initial export.
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

// A second run must not duplicate seeds, must keep a user-edited AGENTS.md, and
// must refresh a stale (older-version) extension file.
func TestPiInstallIdempotentAndRefreshes(t *testing.T) {
	cfg := piTestEnv(t)
	if code := cmdPi([]string{"install"}); code != 0 {
		t.Fatalf("first install exit code = %d", code)
	}

	agents := filepath.Join(cfg, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(cfg, "extensions", "nt-memory.ts")
	if err := os.WriteFile(ext, []byte("// stale old version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := cmdPi([]string{"install"}); code != 0 {
		t.Fatalf("second install exit code = %d", code)
	}

	if data, _ := os.ReadFile(agents); string(data) != "my own rules\n" {
		t.Errorf("user AGENTS.md was clobbered: %q", string(data))
	}
	if data, _ := os.ReadFile(ext); strings.Contains(string(data), "stale old version") {
		t.Error("extension was not refreshed on re-run")
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

func TestPiInstallPrintWritesNothing(t *testing.T) {
	cfg := piTestEnv(t)
	if code := cmdPi([]string{"install", "--print"}); code != 0 {
		t.Fatalf("print exit code = %d", code)
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Errorf("--print created the config dir: %v", err)
	}
}

func TestPiConfigDirRespectsOverride(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "/tmp/custom-pi-dir")
	got, err := piConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom-pi-dir" {
		t.Errorf("piConfigDir() = %q, want the PI_CODING_AGENT_DIR override", got)
	}
}
