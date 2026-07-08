package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/navbytes/nt/internal/note"
)

// seedMemoryStore seeds the rules/ and memory/ folders with one editable example
// note each (only when the folder is empty) and writes the initial nt-rules.md
// export for file-mode users (harmless in the default system-injection mode).
// It's shared by every AI-client integration installer (OpenCode, Pi, …); the
// source label distinguishes what each seeds. cfgDir is the client's config
// directory, where nt-rules.md is written.
func seedMemoryStore(cfgDir, source string) int {
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
		n, err := note.Create(e.S, title, body, []string{tag}, source, folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s install: seed %s/: %v (skipping)\n", source, folder, err)
			return
		}
		n.Extra = append(n.Extra, "description: "+desc)
		if err := n.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "%s install: seed %s/: %v\n", source, folder, err)
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
		fmt.Fprintf(os.Stderr, "%s install: initial nt-rules.md export failed (continuing)\n", source)
	}
	return 0
}
