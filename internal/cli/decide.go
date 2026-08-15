package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/workstream"
)

// cmdDecide records WHY a note changed: it prepends a dated bullet to the
// note's ## Decisions section (memory-dynamics spec §5) — the coarse,
// always-visible version history that keeps in-place edits from erasing the
// story. One line per decision, not per edit; the per-edit record stays in
// git (`nt history`).
func cmdDecide(args []string) int {
	fs := flag.NewFlagSet("decide", flag.ContinueOnError)
	expectMtime := fs.String("expect-mtime", "", "optional: the mtime from a prior `nt show --json` — refuse instead of overwriting if the note changed on disk since")
	flags, positional := splitArgs(args, nil)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) < 2 {
		return usageErr(fmt.Errorf(`decide: need a note handle and the decision, e.g. nt decide jwt-token-lifetime "switched refresh 7d -> 30d because mobile re-auth"`))
	}
	handle, text := positional[0], strings.Join(positional[1:], " ")
	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	n, err := resolveNote(notes, handle)
	if err != nil {
		return fail(fmt.Errorf("decide: %w", err))
	}
	beforeRaw, _ := os.ReadFile(n.Path)
	if err := note.AppendDecision(n, time.Now().Format("2006-01-02"), text); err != nil {
		return usageErr(fmt.Errorf("decide: %w", err))
	}
	n.Updated = time.Now().Format(time.RFC3339)
	if err := n.SaveIfUnchanged(strings.TrimSpace(*expectMtime)); err != nil {
		var stale *note.StaleNoteError
		if errors.As(err, &stale) {
			return fail(fmt.Errorf("decide: %s changed on disk since you loaded it — `nt show %s --json` for the current mtime, then retry", shortID(n.ID), shortID(n.ID)))
		}
		return fail(err)
	}
	if beforeRaw != nil {
		_ = note.RecordUndo(e.S, note.UndoEntry{
			Op: "recorded decision on", TS: time.Now().UTC().Format(time.RFC3339Nano), WS: workstream.Env(),
			Path: n.Path, Before: string(beforeRaw),
		})
	}
	count, _ := n.DecisionStats()
	fmt.Printf("decided %s  %s  (%d decision(s) on record)\n", shortID(n.ID), n.Rel, count)
	return 0
}

// cmdHistory is the fine-grained history channel: a read-only view over the
// git layer nt git-init/nt sync establish — no new storage, no shadow log.
// Default is one line per commit touching the note; --patch is the explicit
// escalation to full diffs.
func cmdHistory(args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	patch := fs.Bool("patch", false, "show full diffs per commit (git log -p), not just the one-line summary")
	since := fs.String("since", "", "limit to commits newer than this (git-style: 30d → '30 days ago', or any git --since value)")
	flags, positional := splitArgs(args, map[string]bool{"patch": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("history: need a note handle (slug/title/id)"))
	}
	e, ok := engine()
	if !ok {
		return 1
	}
	if _, err := os.Stat(filepath.Join(e.S.Dir, ".git")); os.IsNotExist(err) {
		return fail(fmt.Errorf("history: the store isn't a git repo — run `nt git-init` once, and every note edit becomes history for free"))
	}
	notes, _ := note.List(e.S)
	n, err := resolveNote(notes, positional[0])
	if err != nil {
		return fail(fmt.Errorf("history: %w", err))
	}
	rel, rerr := filepath.Rel(e.S.Dir, n.Path)
	if rerr != nil {
		return fail(rerr)
	}
	gitArgs := []string{"log", "--follow"}
	if *patch {
		gitArgs = append(gitArgs, "-p")
	} else {
		gitArgs = append(gitArgs, "--oneline", "--date=short", "--pretty=format:%h  %ad  %s")
	}
	if s := strings.TrimSpace(*since); s != "" {
		gitArgs = append(gitArgs, "--since", gitSince(s))
	}
	gitArgs = append(gitArgs, "--", rel)
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = e.S.Dir
	out, gerr := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if gerr != nil {
		return fail(fmt.Errorf("history: git log: %s", text))
	}
	if text == "" {
		if strings.TrimSpace(*since) != "" {
			// An empty FILTERED result must not claim the note was never
			// committed — it may have plenty of history outside the window.
			fmt.Printf("no commits touch %s in that --since window — drop --since for the full history\n", n.Rel)
		} else {
			fmt.Printf("no history for %s yet — it hasn't been committed (nt sync, or git add/commit in the store)\n", n.Rel)
		}
		return 0
	}
	fmt.Println(text)
	return 0
}

// gitSince turns nt's compact Nd/Nw/Nm/Ny duration shorthand into git's
// "--since" phrasing; anything else passes through verbatim (git accepts
// dates and phrases like "2 weeks ago" natively).
func gitSince(s string) string {
	if len(s) >= 2 {
		if num := s[:len(s)-1]; strings.IndexFunc(num, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			switch s[len(s)-1] {
			case 'd':
				return num + " days ago"
			case 'w':
				return num + " weeks ago"
			case 'm':
				return num + " months ago"
			case 'y':
				return num + " years ago"
			}
		}
	}
	return s
}
