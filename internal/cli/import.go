package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/navbytes/nt/internal/note"
)

// cmdImport is the inverse `nt export` has never had: bulk-load notes from
// either the JSON `nt export --format json` produces (round-trip a backup
// into a fresh store) or a directory of markdown files — including an
// Obsidian vault, since nt's note format already reads Obsidian's frontmatter
// conventions (SPEC §5). Each import creates a FRESH note (a new id) rather
// than resurrecting the source's; a title/tag near-duplicate of an existing
// note is skipped by default, so re-running the same import is a no-op.
//
//	nt import backup.json                         # round-trip an `nt export --format json` backup
//	nt import ~/ObsidianVault/notes               # bulk-load a folder of markdown files
//	nt import ~/vault --folder imported --tag from-obsidian --dry-run
func cmdImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	folder := fs.String("folder", "", "file every imported note under this notes/ subfolder")
	source := fs.String("source", "import", "source stamped on every imported note")
	dryRun := fs.Bool("dry-run", false, "report what would be imported without writing")
	force := fs.Bool("force", false, "import even where a near-duplicate title already exists")
	asJSON := fs.Bool("json", false, "print the import summary as JSON")
	var tags stringSlice
	fs.Var(&tags, "tag", "tag every imported note (repeatable)")
	flags, positional := splitArgs(args, map[string]bool{"dry-run": true, "force": true, "json": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("import: usage: nt import <file.json|directory>"))
	}
	src := positional[0]
	info, serr := os.Stat(src)
	if serr != nil {
		return fail(fmt.Errorf("import: %w", serr))
	}

	e, ok := engine()
	if !ok {
		return 1
	}
	existing := note.Active(mustNotes(e))

	var items []importItem
	var cerr error
	if info.IsDir() {
		items, cerr = collectMarkdownVault(src)
	} else {
		items, cerr = collectJSONExport(src)
	}
	if cerr != nil {
		return fail(fmt.Errorf("import: %w", cerr))
	}
	if len(items) == 0 {
		fmt.Println("import: nothing to import")
		return 0
	}

	var created, skipped []string
	for _, it := range items {
		if strings.TrimSpace(it.Title) == "" {
			continue
		}
		allTags := dedupStrings(append(append([]string{}, it.Tags...), tags...))
		if !*force {
			// Imported items have no project concept (see importItem) — "" is a
			// no-op for FindSimilar's project-aware tag set.
			if sim := note.FindSimilar(existing, it.Title, allTags, ""); len(sim) > 0 {
				skipped = append(skipped, it.Title)
				continue
			}
		}
		if *dryRun {
			created = append(created, it.Title)
			continue
		}
		n, nerr := note.Create(e.S, it.Title, it.Body, allTags, *source, *folder)
		if nerr != nil {
			return fail(fmt.Errorf("import %q: %w", it.Title, nerr))
		}
		changed := false
		if it.Description != "" {
			n.Extra = append(n.Extra, "description: "+it.Description)
			changed = true
		}
		if len(it.Aliases) > 0 {
			n.Aliases = it.Aliases
			changed = true
		}
		if it.ValidFrom != "" {
			n.ValidFrom = it.ValidFrom
			changed = true
		}
		if it.ValidUntil != "" {
			n.ValidUntil = it.ValidUntil
			changed = true
		}
		if changed {
			if serr := n.Save(); serr != nil {
				return fail(fmt.Errorf("import %q: %w", it.Title, serr))
			}
		}
		existing = append(existing, n) // later items in this run dedup against earlier ones
		created = append(created, it.Title)
	}

	if *asJSON {
		return printJSON(map[string]any{"imported": len(created), "skipped": skipped, "dryRun": *dryRun})
	}
	verb := "imported"
	if *dryRun {
		verb = "would import"
	}
	fmt.Printf("%s %d note(s)", verb, len(created))
	if len(skipped) > 0 {
		fmt.Printf(", skipped %d near-duplicate(s) (--force to import anyway)", len(skipped))
	}
	fmt.Println()
	return 0
}

// importItem is a source-agnostic note ready to create — collectMarkdownVault
// and collectJSONExport both produce these from their respective formats.
type importItem struct {
	Title, Body, Description string
	Tags, Aliases            []string
	ValidFrom, ValidUntil    string
}

// collectMarkdownVault walks dir for *.md files — an Obsidian vault or any
// folder of markdown notes — and loads each via note.Load, which already
// tolerates Obsidian's frontmatter conventions (SPEC §5): bare-comma/
// block-list tags, aliases:, and a missing H1 falling back to title:/filename.
// A file that fails to load is skipped, not fatal — one bad file shouldn't
// abort a whole vault import.
func collectMarkdownVault(dir string) ([]importItem, error) {
	var items []importItem
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir // .obsidian/, .git/, etc.
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		n, lerr := note.Load(path)
		if lerr != nil {
			return nil
		}
		items = append(items, importItem{
			Title: n.Title, Body: n.Body, Tags: n.Tags, Aliases: n.Aliases,
			Description: n.Description(1 << 20),
			ValidFrom:   n.ValidFrom, ValidUntil: n.ValidUntil,
		})
		return nil
	})
	return items, err
}

// exportedNote is the subset of `nt export --format json`'s note shape (see
// notesToJSON) that import round-trips; fields nt regenerates on create
// (id, path, mtime, created) are deliberately not read back.
type exportedNote struct {
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	ValidFrom   string   `json:"validFrom"`
	ValidUntil  string   `json:"validUntil"`
}

// collectJSONExport reads `nt export --format json`'s output — an object with
// a "notes" array — and turns each entry into an importItem. A bare JSON
// array of the same note shape is also accepted, so a hand-built or
// third-party JSON list works too.
func collectJSONExport(path string) ([]importItem, error) {
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var payload struct {
		Notes []exportedNote `json:"notes"`
	}
	var notes []exportedNote
	if err := json.Unmarshal(data, &payload); err != nil || len(payload.Notes) == 0 {
		var bare []exportedNote
		if berr := json.Unmarshal(data, &bare); berr != nil {
			return nil, fmt.Errorf("not a valid nt export JSON (expected {\"notes\":[...]} or a bare array): %w", berr)
		} else if len(bare) == 0 {
			return nil, nil // valid JSON, but no notes in it — cmdImport reports "nothing to import"
		}
		notes = bare
	} else {
		notes = payload.Notes
	}
	items := make([]importItem, 0, len(notes))
	for _, n := range notes {
		if strings.TrimSpace(n.Title) == "" {
			continue
		}
		items = append(items, importItem{
			Title: n.Title, Body: n.Body, Description: n.Description, Tags: n.Tags,
			ValidFrom: n.ValidFrom, ValidUntil: n.ValidUntil,
		})
	}
	return items, nil
}

// dedupStrings drops duplicate/empty entries while preserving first-seen order.
func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
