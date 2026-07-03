package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	opencodeassets "github.com/navbytes/nt/integrations/opencode"
	"github.com/navbytes/nt/internal/note"
)

// cmdOpencode dispatches the `nt opencode` namespace.
//
//	nt opencode install          # full integration setup (see cmdOpencodeInstall)
//	nt opencode install --print  # show what would be done, change nothing
func cmdOpencode(args []string) int {
	if len(args) == 0 {
		return usageErr(fmt.Errorf("opencode: a subcommand is required (try `nt opencode install`)"))
	}
	switch args[0] {
	case "install":
		return cmdOpencodeInstall(args[1:])
	default:
		return usageErr(fmt.Errorf("opencode: unknown subcommand %q (supported: install)", args[0]))
	}
}

// opencodeAssets maps each embedded integration file to its destination under
// the OpenCode config dir. These are overwritten on every run, which is the
// upgrade story: re-running `nt opencode install` after an nt upgrade refreshes
// the plugin/skill/commands to the versions this binary shipped with.
var opencodeAssets = []struct{ src, dst string }{
	{"plugins/nt-memory.ts", filepath.Join("plugins", "nt-memory.ts")},
	{"skills/nt/SKILL.md", filepath.Join("skills", "nt", "SKILL.md")},
	{"commands/learn.md", filepath.Join("commands", "learn.md")},
	{"commands/recall.md", filepath.Join("commands", "recall.md")},
}

// cmdOpencodeInstall performs the complete nt ↔ OpenCode integration setup —
// the built-in, cross-platform equivalent of integrations/opencode/install.sh:
//
//  1. register nt's MCP server                    → opencode.json mcp.nt
//  2. install the nt-memory plugin                → plugins/nt-memory.ts
//  3. install the nt skill                        → skills/nt/SKILL.md
//  4. install the /learn and /recall commands     → commands/*.md
//  5. install a starter AGENTS.md                 (only if none exists)
//  6. allow the nt skill                          → opencode.json permission.skill.nt
//  7. seed the rules/ + memory/ store folders and an initial nt-rules.md export
//
// Idempotent and safe to re-run; --print shows every step without writing.
func cmdOpencodeInstall(args []string) int {
	fs := flag.NewFlagSet("opencode install", flag.ContinueOnError)
	print1 := fs.Bool("print", false, "show what would be done, change nothing")
	dryRun := fs.Bool("dry-run", false, "alias for --print")
	flags, _ := splitArgs(args, map[string]bool{"print": true, "dry-run": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	printOnly := *print1 || *dryRun

	cfgPath, err := opencodeConfigPath()
	if err != nil {
		return fail(err)
	}
	cfgDir := filepath.Dir(cfgPath)
	bin, err := ntBinaryPath()
	if err != nil {
		return fail(err)
	}

	// 1. MCP server registration (shares the `nt mcp install --client opencode` path).
	if code := installOpencode(cfgPath, bin, printOnly); code != 0 {
		return code
	}

	// 2–4. Plugin, skill, commands — from the embedded bundle.
	for _, a := range opencodeAssets {
		data, rerr := opencodeassets.Assets.ReadFile(a.src)
		if rerr != nil {
			return fail(fmt.Errorf("opencode install: embedded asset %s: %w", a.src, rerr))
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
			return fail(fmt.Errorf("opencode install: %w", err))
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fail(fmt.Errorf("opencode install: write %s: %w", dst, err))
		}
	}

	// 5. AGENTS.md — never clobber an existing one (it's user-owned prose).
	agents := filepath.Join(cfgDir, "AGENTS.md")
	if _, serr := os.Stat(agents); serr == nil {
		fmt.Printf("keeping your existing %s (the nt starter is in the repo's integrations/opencode/AGENTS.md)\n", agents)
	} else {
		data, rerr := opencodeassets.Assets.ReadFile("AGENTS.md")
		if rerr != nil {
			return fail(fmt.Errorf("opencode install: embedded AGENTS.md: %w", rerr))
		}
		if printOnly {
			fmt.Printf("would install %s (only because none exists)\n", agents)
		} else {
			if err := os.WriteFile(agents, data, 0o644); err != nil {
				return fail(fmt.Errorf("opencode install: write %s: %w", agents, err))
			}
			fmt.Printf("installed   %s\n", agents)
		}
	}

	// 6. permission.skill.nt = allow (respects an explicit user value, even "deny").
	if printOnly {
		fmt.Printf("would ensure permission.skill.nt = \"allow\" in %s\n", cfgPath)
	} else {
		changed, err := ensureOpencodeSkillPermission(cfgPath)
		if err != nil {
			return fail(err)
		}
		if changed {
			fmt.Printf("allowed the nt skill in %s (permission.skill.nt)\n", cfgPath)
		}
	}

	// 7. Seed the always-in-context folders + the initial rules export. The seeds
	// are editable examples; the folders + tags are the convention that matters.
	if printOnly {
		fmt.Println("would seed the rules/ and memory/ nt folders (if empty) and export nt-rules.md")
		return 0
	}
	if code := seedOpencodeStore(cfgDir); code != 0 {
		return code
	}

	fmt.Println("\n✓ Done. Restart OpenCode (or reload its MCP servers) to pick up nt.")
	fmt.Println("  rules:  nt note \"<rule>\" --kind rule --description \"…\"        (injected every turn — keep small)")
	fmt.Println("  memory: nt note \"<fact>\" --kind memory --description \"…\"")
	fmt.Println("  check what gets injected: nt export --tag rule --title Rules")
	return 0
}

// ensureOpencodeSkillPermission sets permission.skill.nt = "allow" in the
// OpenCode config, creating the nesting as needed and preserving every other
// key. An existing permission.skill.nt value — whatever it is — is respected.
func ensureOpencodeSkillPermission(path string) (changed bool, err error) {
	root, fresh, err := readConfigMap(path)
	if err != nil {
		return false, err
	}
	perm, _ := root["permission"].(map[string]any)
	if perm == nil {
		if _, exists := root["permission"]; exists {
			return false, fmt.Errorf("%s has a non-object \"permission\" value; fix it and re-run", path)
		}
		perm = map[string]any{}
	}
	skill, _ := perm["skill"].(map[string]any)
	if skill == nil {
		if _, exists := perm["skill"]; exists {
			return false, fmt.Errorf("%s has a non-object \"permission.skill\" value; fix it and re-run", path)
		}
		skill = map[string]any{}
	}
	if _, exists := skill["nt"]; exists {
		return false, nil // user already chose a value — leave it alone
	}
	skill["nt"] = "allow"
	perm["skill"] = skill
	root["permission"] = perm
	if fresh {
		if _, ok := root["$schema"]; !ok {
			root["$schema"] = opencodeSchema
		}
	}
	if err := writeConfigAtomic(path, root); err != nil {
		return false, err
	}
	return true, nil
}

// seedOpencodeStore seeds the rules/ and memory/ folders with one editable
// example note each (only when the folder is empty) and writes the initial
// nt-rules.md export for file-mode users (harmless in system-injection mode).
func seedOpencodeStore(cfgDir string) int {
	e, ok := engine()
	if !ok {
		return 1
	}
	notes := note.Active(mustNotes(e))
	hasFolder := func(prefix string) bool {
		for _, n := range notes {
			if strings.HasPrefix(n.Rel, prefix+"/") {
				return true
			}
		}
		return false
	}
	seed := func(title, desc, body, folder, tag string) {
		n, err := note.Create(e.S, title, body, []string{tag}, "opencode", folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "opencode install: seed %s/: %v (skipping)\n", folder, err)
			return
		}
		n.Extra = append(n.Extra, "description: "+desc)
		if err := n.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "opencode install: seed %s/: %v\n", folder, err)
			return
		}
		// Rel is only set by note.List; Path is what Create fills in.
		fmt.Printf("seeded      %s/ (%s — edit or remove)\n", folder, filepath.Base(n.Path))
	}
	if !hasFolder("rules") {
		seed(
			"Output style: terse factual bullets",
			"How the agent should phrase answers by default",
			"- Answer in bullet points, not prose.\n"+
				"- Plain, direct words. No filler, hedging, or fancy phrasing.\n"+
				"- Lead with the fact/answer; skip preamble and restating the question.\n"+
				"- Elaborate only when asked.",
			"rules", "rule",
		)
	}
	if !hasFolder("memory") {
		seed(
			"Project + user facts the agent should always know",
			"Durable user preferences and project conventions (edit me)",
			"Edit this note (or add siblings tagged memory-core) with durable preferences and conventions.",
			"memory", "memory-core",
		)
	}
	// Initial export; a failure here shouldn't fail the install (mirrors install.sh).
	if code := cmdExport([]string{"--tag", "rule", "--title", "Rules", "--out", filepath.Join(cfgDir, "nt-rules.md")}); code != 0 {
		fmt.Fprintln(os.Stderr, "opencode install: initial nt-rules.md export failed (continuing)")
	}
	return 0
}
