// Package cli implements nt's command-line interface. Every mutating command
// goes through the shared mutation engine (internal/mutate), so the CLI and the
// future TUI write tasks.txt through exactly the same locked, journaled path.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mutate"
)

// Version is the build version, set from main (via -ldflags).
var Version = "dev"

// Run dispatches a subcommand and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		return cmdDefault()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add", "a":
		return cmdAdd(rest)
	case "note":
		return cmdNote(rest)
	case "list", "ls":
		return cmdList(rest)
	case "ready":
		return cmdReady(rest)
	case "recall":
		return cmdRecall(rest)
	case "log":
		return cmdLog(rest)
	case "done", "do":
		return cmdDone(rest)
	case "update", "up":
		return cmdUpdate(rest)
	case "search", "q":
		return cmdSearch(rest)
	case "links":
		return cmdLinks(rest)
	case "rm", "delete":
		return cmdRm(rest)
	case "archive":
		return cmdArchive(rest)
	case "undo":
		return cmdUndo(rest)
	case "edit":
		return cmdEdit(rest)
	case "path":
		return cmdPath(rest)
	case "hook":
		return cmdHook(rest)
	case "version", "--version", "-v":
		fmt.Println("nt " + Version)
		return 0
	case "help", "-h", "--help":
		printHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nt: unknown command %q (try `nt help`)\n", cmd)
		return 2
	}
}

// engine opens the store + mutation engine, reporting errors uniformly.
func engine() (*mutate.Engine, bool) {
	e, err := mutate.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nt: %v\n", err)
		return nil, false
	}
	return e, true
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "nt: %v\n", err)
	return 1
}

// --- argument splitting --------------------------------------------------

// splitArgs separates flag tokens from positional tokens so flags may appear
// before or after the positional title (Go's flag package stops at the first
// positional otherwise). boolFlags names the flags that take no value.
func splitArgs(args []string, boolFlags map[string]bool) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if j := strings.IndexByte(name, '='); j >= 0 {
				continue // --key=value, self-contained
			}
			if boolFlags[name] {
				continue // boolean, no value follows
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

// stringSlice is a repeatable string flag (e.g. --tag a --tag b).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// shortID is a compact, distinguishing handle for display: the last 6 chars of
// the ULID. ULIDs share a long leading timestamp prefix for items created close
// together, so the entropy *tail* is what tells them apart. Resolution accepts
// this suffix (see Doc.Resolve).
func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

// parseDate and parsePriority delegate to the shared dateparse package so the
// CLI and TUI accept identical inputs (SPEC §7.3).
func parseDate(s string) (string, bool)   { return dateparse.Date(s) }
func parsePriority(s string) (byte, bool) { return dateparse.Priority(s) }

func printHelp() {
	fmt.Print(helpText)
}

const helpText = `nt — terminal task & note manager (durable memory for AI sessions)

USAGE
  nt                          open the interactive TUI
  nt add "title" [flags]      add a task
  nt note "title" [flags]     capture a note
  nt list [flags]             list tasks            (alias: ls)
  nt ready [flags]            open, unblocked tasks by urgency — start here
  nt recall [flags]           read back prior items (for AI sessions)
  nt log [--since|--days N]    completed tasks, newest first (the Logbook)
  nt done <id|task:N>         mark a task done       (alias: do)
  nt update <id|task:N> ...   change a task          (alias: up)
  nt search "query" [flags]   full-text search       (alias: q)
  nt links <id|task:N>        forward links + backlinks
  nt edit <id|task:N>         edit a task/note in $EDITOR
  nt rm <id…>                 delete tasks (undoable)
  nt archive                  move done tasks to done.txt
  nt undo                     revert the last change
  nt path                     print the store directory
  nt hook                     sync a Claude Code TodoWrite event (PostToolUse hook)

ADD/UPDATE FLAGS
  --pri high|med|low   --due today|tomorrow|fri|+3d|YYYY-MM-DD
  --tag NAME (repeat)  --project NAME   --source NAME
  --parent <id>        --blocks <id>    --note <slug>   (link to a note)

LIST/RECALL FLAGS
  --status open|doing|blocked|done   --tag NAME   --project NAME
  --sort urgency|due|created         --all        --json
  --show-blocked                     --source NAME / --since YYYY-MM-DD (recall)

Recurring: add --recur weekly|3d|… ; completing spawns the next occurrence.
Dependencies: add --blocks <id> ; blocked tasks hide unless --show-blocked.

The store lives at $NT_DIR (default ~/.local/share/nt): tasks.txt + notes/*.md.
`
