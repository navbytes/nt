package cli

// Export drift tracking: `nt export --out <file>` compiles notes into a file
// that some always-in-context layer loads (CLAUDE.md imports, OpenCode
// instructions, a SKILL.md body). That file is a snapshot — prune a rule with
// /nt-distill or add one with /nt-learn and the compiled copy is silently
// stale, still paying tokens for what was pruned and never delivering what was
// added. Nothing re-runs export automatically on this surface (unlike the
// OpenCode/Pi plugins, which re-export per session), so the store remembers
// WHAT was exported WHERE (export-state.json, beside views.json) and
// `nt doctor` re-renders each recorded selection against the current store to
// flag targets that no longer match.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/store"
)

// exportSelection captures WHICH content an export compiled and HOW it was
// rendered — everything needed to re-render the same export later and compare.
type exportSelection struct {
	Folder          string   `json:"folder,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Type            string   `json:"type,omitempty"`
	Format          string   `json:"format,omitempty"`
	Title           string   `json:"title,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	IncludeArchived bool     `json:"include_archived,omitempty"`
	NoProvenance    bool     `json:"no_provenance,omitempty"`
	NoHeader        bool     `json:"no_header,omitempty"`
}

// commandLine reconstructs the `nt export …` invocation that refreshes this
// selection — shown in doctor's drift warning so the fix is copy-pasteable.
func (sel exportSelection) commandLine(out string) string {
	var b strings.Builder
	b.WriteString("nt export")
	for _, t := range sel.Tags {
		fmt.Fprintf(&b, " --tag %s", t)
	}
	if sel.Folder != "" {
		fmt.Fprintf(&b, " --folder %s", sel.Folder)
	}
	if sel.Type != "" && sel.Type != "note" {
		fmt.Fprintf(&b, " --type %s", sel.Type)
	}
	if sel.Format != "" && sel.Format != "md" {
		fmt.Fprintf(&b, " --format %s", sel.Format)
	}
	if sel.Title != "" {
		fmt.Fprintf(&b, " --title %q", sel.Title)
	}
	if sel.Limit > 0 {
		fmt.Fprintf(&b, " --limit %d", sel.Limit)
	}
	if sel.IncludeArchived {
		b.WriteString(" --include-archived")
	}
	if sel.NoProvenance {
		b.WriteString(" --no-provenance")
	}
	if sel.NoHeader {
		b.WriteString(" --no-header")
	}
	fmt.Fprintf(&b, " --out %s", out)
	return b.String()
}

// exportRecord is one tracked export target.
type exportRecord struct {
	Out       string          `json:"out"` // absolute path of the exported file
	Selection exportSelection `json:"selection"`
	Hash      string          `json:"hash"`     // sha256 of the file as written
	Exported  string          `json:"exported"` // RFC3339 of the last export
}

// exportStateFile is the tracking file's basename within $NT_DIR — a plain,
// hand-editable JSON file beside views.json, same ethos: state you can read,
// fix, or delete with a text editor.
const exportStateFile = "export-state.json"

type exportState struct {
	Exports []exportRecord `json:"exports"`
}

func loadExportState(dir string) exportState {
	var st exportState
	data, err := os.ReadFile(filepath.Join(dir, exportStateFile))
	if err != nil {
		return st // missing (or unreadable) = nothing tracked
	}
	_ = json.Unmarshal(data, &st) // malformed = nothing tracked; recordExport rewrites it whole
	return st
}

func saveExportState(dir string, st exportState) error {
	sort.SliceStable(st.Exports, func(i, j int) bool { return st.Exports[i].Out < st.Exports[j].Out })
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteAtomic(filepath.Join(dir, exportStateFile), append(data, '\n'), 0o644)
}

// exportHash fingerprints rendered export content. The rendered string must be
// the exact bytes written (trailing newline included) so a byte-compare of the
// file on disk against a re-render is meaningful.
func exportHash(rendered string) string {
	sum := sha256.Sum256([]byte(rendered))
	return hex.EncodeToString(sum[:])
}

// recordExport upserts the tracking record for one --out target. Best-effort
// by contract: callers warn on error but never fail the export itself.
func recordExport(dir, out string, sel exportSelection, rendered string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}
	st := loadExportState(dir)
	rec := exportRecord{
		Out: abs, Selection: sel, Hash: exportHash(rendered),
		Exported: time.Now().Format(time.RFC3339),
	}
	replaced := false
	for i := range st.Exports {
		if st.Exports[i].Out == abs {
			st.Exports[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		st.Exports = append(st.Exports, rec)
	}
	return saveExportState(dir, st)
}

// exportDriftWarnings re-renders every tracked export against the current
// store and reports targets that no longer match — the doctor check for "the
// compiled rules file is stale". Read-only; the fix is the printed command
// (or hand-editing export-state.json to stop tracking a target).
func exportDriftWarnings(e *mutate.Engine) []string {
	st := loadExportState(e.S.Dir)
	var warns []string
	for _, rec := range st.Exports {
		current, err := os.ReadFile(rec.Out)
		if errors.Is(err, fs.ErrNotExist) {
			warns = append(warns, fmt.Sprintf("exported file %s is gone — re-export (`%s`) or drop its entry from %s",
				rec.Out, rec.Selection.commandLine(rec.Out), exportStateFile))
			continue
		}
		if err != nil {
			warns = append(warns, fmt.Sprintf("exported file %s is unreadable (%v)", rec.Out, err))
			continue
		}
		rendered, _, _, rerr := renderExport(e, rec.Selection)
		if rerr != nil {
			warns = append(warns, fmt.Sprintf("could not re-render export for %s: %v", rec.Out, rerr))
			continue
		}
		if !strings.HasSuffix(rendered, "\n") {
			rendered += "\n" // the write path normalizes before writing; compare like for like
		}
		if exportHash(string(current)) == exportHash(rendered) {
			continue // fresh: the file is exactly what the store renders today
		}
		msg := fmt.Sprintf("export drift: %s no longer matches the store — re-run `%s`", rec.Out, rec.Selection.commandLine(rec.Out))
		if exportHash(string(current)) != rec.Hash {
			// The file itself changed since nt wrote it — hand edits will be
			// overwritten by a re-export; say so instead of silently clobbering.
			msg += " (the file was also edited outside nt export — re-exporting overwrites those edits)"
		}
		warns = append(warns, msg)
	}
	return warns
}
