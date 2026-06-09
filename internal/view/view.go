// Package view persists named "smart views" — saved bundles of the `nt list`
// filter so a user can capture a query once and recall it by name (T4). Views
// live in $NT_DIR/views.json, a machine-managed file (like undo.jsonl), kept
// separate from the hand-edited, read-only config.toml so `nt view save` never
// rewrites a user's annotated config. The package is surface-agnostic: it knows
// only the filter shape, not how any front-end renders a list, so the CLI, TUI,
// and web can all share these specs.
package view

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/navbytes/nt/internal/store"
)

// FileName is the views file's basename within $NT_DIR.
const FileName = "views.json"

// Spec is one saved view: the subset of `nt list` flags that select and order
// tasks. Output-only choices (e.g. --json) are deliberately excluded — they are
// a recall-time concern, not part of the saved query.
type Spec struct {
	Status      string `json:"status,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Project     string `json:"project,omitempty"`
	Sort        string `json:"sort,omitempty"`
	All         bool   `json:"all,omitempty"`
	ShowBlocked bool   `json:"showBlocked,omitempty"`
	Tree        bool   `json:"tree,omitempty"`
}

// file is the on-disk envelope. The wrapper object (rather than a bare map)
// leaves room to add metadata/versioning later without a format break.
type file struct {
	Views map[string]Spec `json:"views"`
}

// reserved names can't be used for a view because they're `nt view`
// subcommands — `nt view <reserved>` would dispatch the subcommand, not recall.
var reserved = map[string]bool{
	"save": true, "recall": true, "list": true, "ls": true, "rm": true, "delete": true, "show": true,
}

// ValidName reports whether s is usable as a view name: non-empty, no spaces or
// path/glob characters, and not a reserved subcommand word.
func ValidName(s string) error {
	if s == "" {
		return fmt.Errorf("view name is empty")
	}
	if reserved[s] {
		return fmt.Errorf("%q is a reserved word; pick another name", s)
	}
	if strings.ContainsAny(s, " \t/\\*?\"'") {
		return fmt.Errorf("view name %q may not contain spaces or any of / \\ * ? \" '", s)
	}
	return nil
}

// Load reads $NT_DIR/views.json. A missing file yields an empty (non-nil) map
// and no error, so callers can treat "no views yet" as the normal first run.
func Load(dir string) (map[string]Spec, error) {
	data, err := store.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]Spec{}, nil
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if f.Views == nil {
		f.Views = map[string]Spec{}
	}
	return f.Views, nil
}

// Save writes the view set atomically. An empty map is written as `{}` rather
// than removing the file, keeping the path stable.
func Save(dir string, views map[string]Spec) error {
	data, err := json.MarshalIndent(file{Views: views}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return store.WriteAtomic(filepath.Join(dir, FileName), data, 0o644)
}

// Names returns the saved view names sorted alphabetically.
func Names(views map[string]Spec) []string {
	names := make([]string, 0, len(views))
	for n := range views {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Args renders a Spec back into the `nt list` flags it represents, in a stable
// order. It is the inverse of capturing flags, used both to show a view's
// definition (`nt view list`) and to document what a recall will run. An empty
// Spec (the all-open default) renders as no flags.
func (s Spec) Args() []string {
	var a []string
	if s.Status != "" {
		a = append(a, "--status", s.Status)
	}
	if s.Tag != "" {
		a = append(a, "--tag", s.Tag)
	}
	if s.Project != "" {
		a = append(a, "--project", s.Project)
	}
	if s.Sort != "" {
		a = append(a, "--sort", s.Sort)
	}
	if s.All {
		a = append(a, "--all")
	}
	if s.ShowBlocked {
		a = append(a, "--show-blocked")
	}
	if s.Tree {
		a = append(a, "--tree")
	}
	return a
}

// Summary is a one-line human description of a view's flags, e.g.
// "--status open --sort due", or "(all open tasks)" when it has no filters.
func (s Spec) Summary() string {
	if a := s.Args(); len(a) > 0 {
		return strings.Join(a, " ")
	}
	return "(all open tasks)"
}
