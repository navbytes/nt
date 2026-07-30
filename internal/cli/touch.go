package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/workstream"
)

// cmdTouch re-confirms notes: it stamps `reviewed:` with today's date, resetting
// the relevance-decay clock (memory-dynamics spec §3.4) without editing the
// body. Deliberately explicit — reading a note is NOT confirming it, so nothing
// auto-touches; a human or agent states "still true" and the fade resets.
func cmdTouch(args []string) int {
	fs := flag.NewFlagSet("touch", flag.ContinueOnError)
	expectMtime := fs.String("expect-mtime", "", "optional: the mtime from a prior `nt show --json` — refuse instead of overwriting if the note changed on disk since")
	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("touch: need a note handle (slug/title/id), e.g. `nt touch jwt-token-lifetime`"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	today := time.Now().Format("2006-01-02")
	code := 0
	for _, h := range positional {
		n, err := resolveNote(notes, h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "touch: %v\n", err)
			code = 1
			continue
		}
		beforeRaw, _ := os.ReadFile(n.Path)
		n.Reviewed = today
		if err := n.SaveIfUnchanged(strings.TrimSpace(*expectMtime)); err != nil {
			var stale *note.StaleNoteError
			if errors.As(err, &stale) {
				return fail(fmt.Errorf("touch: %s changed on disk since you loaded it — `nt show %s --json` for the current mtime, then retry", shortID(n.ID), shortID(n.ID)))
			}
			return fail(err)
		}
		if beforeRaw != nil {
			_ = note.RecordUndo(e.S, note.UndoEntry{
				Op: "touched", TS: time.Now().UTC().Format(time.RFC3339Nano), WS: workstream.Env(),
				Path: n.Path, Before: string(beforeRaw),
			})
		}
		suffix := ""
		if n.HalfLife == "" {
			suffix = "  (no half_life set — reviewed is recorded, but this note doesn't decay; set one with nt edit --half-life)"
		}
		fmt.Printf("touched %s  %s  reviewed: %s%s\n", shortID(n.ID), n.Rel, today, suffix)
	}
	return code
}
