package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	piassets "github.com/navbytes/nt/integrations/pi"
)

// cmdPi dispatches the `nt pi` namespace.
//
//	nt pi install          # full integration setup (see cmdPiInstall)
//	nt pi install --print  # show what would be done, change nothing
func cmdPi(args []string) int {
	if len(args) == 0 {
		return usageErr(fmt.Errorf("pi: a subcommand is required (try `nt pi install`)"))
	}
	switch args[0] {
	case "install":
		return cmdPiInstall(args[1:])
	default:
		return usageErr(fmt.Errorf("pi: unknown subcommand %q (supported: install)", args[0]))
	}
}

// piConfigDir resolves Pi's coding-agent config directory. Pi reads
// ~/.pi/agent on every platform, overridable with PI_CODING_AGENT_DIR.
func piConfigDir() (string, error) {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

// piAssets maps each embedded integration file to its destination under the Pi
// config dir. These are overwritten on every run, which is the upgrade story:
// re-running `nt pi install` after an nt upgrade refreshes the
// extension/skill/prompts to the versions this binary shipped with.
var piAssets = []struct{ src, dst string }{
	{"extensions/nt-memory.ts", filepath.Join("extensions", "nt-memory.ts")},
	{"skills/nt/SKILL.md", filepath.Join("skills", "nt", "SKILL.md")},
	{"prompts/learn.md", filepath.Join("prompts", "learn.md")},
	{"prompts/recall.md", filepath.Join("prompts", "recall.md")},
}

// cmdPiInstall performs the complete nt ↔ Pi integration setup — the built-in,
// cross-platform equivalent of integrations/pi/install.sh:
//
//  1. install the nt-memory extension            → extensions/nt-memory.ts
//  2. install the nt skill                        → skills/nt/SKILL.md
//  3. install the /learn and /recall prompts      → prompts/*.md
//  4. install a starter AGENTS.md                 (only if none exists)
//  5. seed the rules/ + memory/ store folders and an initial nt-rules.md export
//
// Unlike OpenCode there is NO separate MCP-registration step: Pi has no built-in
// MCP, so the extension itself bridges `nt mcp` in as native Pi tools.
//
// Idempotent and safe to re-run; --print shows every step without writing.
func cmdPiInstall(args []string) int {
	fs := flag.NewFlagSet("pi install", flag.ContinueOnError)
	print1 := fs.Bool("print", false, "show what would be done, change nothing")
	dryRun := fs.Bool("dry-run", false, "alias for --print")
	flags, _ := splitArgs(args, map[string]bool{"print": true, "dry-run": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	printOnly := *print1 || *dryRun

	cfgDir, err := piConfigDir()
	if err != nil {
		return fail(err)
	}

	// 1–3. Extension, skill, prompts — from the embedded bundle.
	for _, a := range piAssets {
		data, rerr := piassets.Assets.ReadFile(a.src)
		if rerr != nil {
			return fail(fmt.Errorf("pi install: embedded asset %s: %w", a.src, rerr))
		}
		dst := filepath.Join(cfgDir, a.dst)
		if printOnly {
			fmt.Printf("would install %s (%d bytes)\n", dst, len(data))
			continue
		}
		switch existing, rerr := os.ReadFile(dst); {
		case rerr == nil && bytes.Equal(existing, data):
			fmt.Printf("up to date  %s\n", dst)
			continue
		case rerr == nil:
			fmt.Printf("updated     %s\n", dst)
		default:
			fmt.Printf("installed   %s\n", dst)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fail(fmt.Errorf("pi install: %w", err))
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fail(fmt.Errorf("pi install: write %s: %w", dst, err))
		}
	}

	// 4. AGENTS.md — never clobber an existing one (it's user-owned prose).
	agents := filepath.Join(cfgDir, "AGENTS.md")
	if _, serr := os.Stat(agents); serr == nil {
		fmt.Printf("keeping your existing %s (the nt starter is in the repo's integrations/pi/AGENTS.md)\n", agents)
	} else {
		data, rerr := piassets.Assets.ReadFile("AGENTS.md")
		if rerr != nil {
			return fail(fmt.Errorf("pi install: embedded AGENTS.md: %w", rerr))
		}
		if printOnly {
			fmt.Printf("would install %s (only because none exists)\n", agents)
		} else {
			if err := os.MkdirAll(cfgDir, 0o755); err != nil {
				return fail(fmt.Errorf("pi install: %w", err))
			}
			if err := os.WriteFile(agents, data, 0o644); err != nil {
				return fail(fmt.Errorf("pi install: write %s: %w", agents, err))
			}
			fmt.Printf("installed   %s\n", agents)
		}
	}

	// 5. Seed the always-in-context folders + the initial rules export. The seeds
	// are editable examples; the folders + tags are the convention that matters.
	if printOnly {
		fmt.Println("would seed the rules/ and memory/ nt folders (if empty) and export nt-rules.md")
		return 0
	}
	if code := seedMemoryStore(cfgDir, "pi"); code != 0 {
		return code
	}

	fmt.Println("\n✓ Done. Restart Pi (or run /reload) to pick up the nt-memory extension.")
	fmt.Println("  The extension bridges `nt mcp` in as native Pi tools (Pi has no built-in MCP).")
	fmt.Println("  rules:  nt note \"<rule>\" --kind rule --description \"…\"        (injected every turn — keep small)")
	fmt.Println("  memory: nt note \"<fact>\" --kind memory --description \"…\"")
	fmt.Println("  check what gets injected: nt export --tag rule --title Rules")
	return 0
}
