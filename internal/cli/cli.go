// Package cli implements nt's command-line interface. Every mutating command
// goes through the shared mutation engine (internal/mutate), so the CLI and the
// future TUI write tasks.txt through exactly the same locked, journaled path.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/navbytes/nt/internal/config"
	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/mcp"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
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
	case "notes":
		return cmdNotes(rest)
	case "show", "cat":
		return cmdShow(rest)
	case "journal", "j":
		return cmdJournal(rest)
	case "list", "ls":
		return cmdList(rest)
	case "view", "views":
		return cmdView(rest)
	case "ready":
		return cmdReady(rest)
	case "today":
		return cmdToday(rest)
	case "agenda":
		return cmdAgenda(rest)
	case "review":
		return cmdReview(rest)
	case "recall":
		return cmdRecall(rest)
	case "log":
		return cmdLog(rest)
	case "done", "do":
		return cmdDone(rest)
	case "skip":
		return cmdSkip(rest)
	case "start":
		return cmdStart(rest)
	case "stop":
		return cmdStop(rest)
	case "update", "up":
		return cmdUpdate(rest)
	case "search", "q":
		return cmdSearch(rest)
	case "tags":
		return cmdTags(rest)
	case "tag":
		return cmdTag(rest)
	case "links":
		return cmdLinks(rest)
	case "mv", "rename":
		return cmdMv(rest)
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
	case "doctor":
		return cmdDoctor(rest)
	case "git-init":
		return cmdGitInit(rest)
	case "hook":
		return cmdHook(rest)
	case "mcp":
		if len(rest) > 0 && rest[0] == "install" {
			return cmdMcpInstall(rest[1:])
		}
		if err := mcp.Serve(Version); err != nil {
			return fail(err)
		}
		return 0
	case "web":
		return cmdWeb(rest)
	case "version", "--version", "-v":
		fmt.Println("nt " + Version)
		return 0
	case "help", "-h", "--help":
		printHelp()
		return 0
	default:
		if s := suggestCommand(cmd); s != "" {
			fmt.Fprintf(os.Stderr, "nt: unknown command %q — did you mean %q? (try `nt help`)\n", cmd, s)
		} else {
			fmt.Fprintf(os.Stderr, "nt: unknown command %q (try `nt help`)\n", cmd)
		}
		return 2
	}
}

// knownCommands lists the dispatchable verbs + aliases, used to suggest a near
// match on a typo. Kept in sync with the switch in the dispatcher above.
var knownCommands = []string{
	"add", "a", "note", "notes", "show", "cat", "journal", "j", "list", "ls",
	"view", "views", "ready", "today", "agenda", "review", "recall", "log",
	"done", "do", "skip", "start", "stop", "update", "up", "search", "q",
	"tags", "tag", "links", "mv", "rename", "rm", "delete", "archive", "undo",
	"edit", "path", "doctor", "git-init", "hook", "mcp", "web", "version", "help",
}

// suggestCommand returns the closest known command to cmd within a small edit
// distance, or "" if nothing is close enough — so a typo like "redy" points to
// "ready" instead of dumping the whole help wall on the user.
func suggestCommand(cmd string) string {
	best, bestDist := "", 1<<30
	// Allow more slack for longer words; cap so unrelated input suggests nothing.
	max := 2
	if len(cmd) >= 6 {
		max = 3
	}
	for _, c := range knownCommands {
		d := levenshtein(cmd, c)
		if d < bestDist {
			bestDist, best = d, c
		}
	}
	if bestDist <= max {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur := make([]int, lb+1)
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
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

// usageErr reports an argument/usage error: same stream + "nt: " prefix as
// fail(), but exit code 2 (reserved for bad invocation; 1 is for operational
// failures). Use it for missing/invalid positional args and flag misuse.
func usageErr(err error) int {
	fmt.Fprintf(os.Stderr, "nt: %v\n", err)
	return 2
}

// interactive reports whether nt is driven by a human at a terminal — both stdin
// AND stdout must be TTYs. Agents and scripts almost always pipe at least one
// (to feed input or capture output), so they read as non-interactive.
func interactive() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// confirm prompts on the terminal for a yes/no answer, defaulting to no. Only
// meaningful interactively — callers gate on interactive() (and a --yes flag)
// before calling, so scripts and agents are never blocked on stdin.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// prompt reads a single lowercased line from the terminal (for multi-choice
// prompts like the delete-with-backlinks flow).
func prompt(msg string) string {
	fmt.Fprint(os.Stderr, msg)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line))
}

// resolveHandle maps a user-supplied task handle to a task, refusing a positional
// "task:N" / bare "N" from non-interactive callers: the index is recomputed each
// run, so an agent that read the list a moment ago may act on the wrong task
// after any concurrent write. Stable ULIDs have no such gap. (Product #8 / §7.2.)
func resolveHandle(d *task.Doc, handle string) (*task.Task, error) {
	if task.IsPositional(handle) && !interactive() {
		return nil, fmt.Errorf("%q is interactive-only — scripts and agents must use the task id "+
			"from `nt list` (the short code or full id:), which is stable across concurrent edits", handle)
	}
	t, amb := d.Resolve(handle)
	if amb {
		return nil, fmt.Errorf("%q is ambiguous", handle)
	}
	if t == nil {
		return nil, fmt.Errorf("no task %q", handle)
	}
	return t, nil
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

// handleArgs returns the positional task handles in args, rejecting any stray
// flag-looking token (e.g. `nt done --json id`) with a clear error rather than
// silently treating it as a handle. cmd is the subcommand name for the message.
func handleArgs(cmd string, args []string) ([]string, error) {
	flags, positional := splitArgs(args, nil)
	if len(flags) > 0 {
		return nil, fmt.Errorf("%s: unknown flag %q", cmd, flags[0])
	}
	return positional, nil
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

// loadConfig reads $NT_DIR/config.toml (resolving the dir without opening the
// store). A missing/broken config yields an empty Config, never an error.
func loadConfig() *config.Config {
	dir, err := store.ResolveDir()
	if err != nil {
		return &config.Config{}
	}
	c, _ := config.Load(dir)
	return c
}

func printHelp() {
	fmt.Print(helpText)
}

const helpText = `nt — tasks & notes as plain text · durable memory for AI agents

USAGE
  nt                          open the interactive TUI
  nt add "title" [flags]      add a task
  nt note "title" [flags]     capture a note (--folder work files it in notes/work/)
  nt notes [--folder|--tag]   list notes (one row each)  (--json)
  nt show <note>              print a note in the terminal (alias: cat)
  nt journal [--date D]       open a daily note in $EDITOR (D: today|fri|+1d|YYYY-MM-DD)  (alias: j)
  nt list [flags]             list tasks            (alias: ls)
  nt view <name>              run a saved view; save/list/rm to manage them
  nt ready [flags]            open, unblocked tasks by urgency — start here
  nt today [flags]            overdue + due-today + just-started, grouped
  nt agenda [--days N]        the next N days, grouped Overdue/Today/Upcoming
  nt review [--stale N]       weekly digest: overdue, stale, undated, stuck projects
  nt recall [flags]           read back prior items (for AI sessions)
  nt log [--since|--days N]    completed tasks, newest first (the Logbook)
  nt done <id|task:N>         mark a task done       (alias: do)
  nt skip <id|task:N>         move a recurring task to its next occurrence
  nt start <id…> / stop <id…> time-track a task (logs elapsed into spent:)
  nt update <id…> [flags]     change one or more tasks (bulk)  (alias: up)
  nt list --tree              show sub-tasks indented under their parent
  nt search "query" [--tag T]  full-text + tag search (AND terms; "phrase"; --json) (alias: q)
  nt tags                     list the tag vocabulary with counts
  nt tag <id|note…> +x -y     retag tasks or notes (no $EDITOR; preserves frontmatter)
  nt links <id|note>          forward links + backlinks + deps  (--orphans, --json)
  nt edit <id|note>           edit a task or note in $EDITOR
  nt mv <note> <new|path>     rename/move a note, updating all [[links]] to it
  nt rm <id|note> [flags]     delete tasks (undoable) or notes (to .trash/)
                              notes: --unlink strips inbound links, --force keeps them;
                              -y/--yes skips the confirm prompt
  nt archive                  move done tasks to done.txt
  nt archive <note> [--undo]  retire a note from the active views (reversible; still on disk)
  nt undo                     revert the last change
  nt path                     print the store directory
  nt version                  print the nt version (alias: -v, --version)
  nt git-init                 set up the store for git (union-merge + .gitignore)
  nt doctor [--check]         reconcile tasks.txt (dedup ids) after a git merge
  nt hook                     sync a Claude Code TodoWrite event (PostToolUse hook)
  nt mcp                      run the MCP server (stdio) — typed tools for agents
  nt mcp install [--client]   register nt with an AI client (claude-code|claude-desktop|opencode)
                              --print/--dry-run shows what would change without writing
  nt web [--port N]           browse and edit notes in a browser (localhost only)
  nt web --detach             run the viewer in the background (--status / --stop to manage)

  (task:N and bare row numbers are a TUI/interactive convenience; scripts and
   agents pass the stable <id> from --json or 'nt list'.)

ADD/UPDATE FLAGS
  --pri high|med|low   --due today|tomorrow|fri|+3d|YYYY-MM-DD
  --tag NAME (repeat)  --project NAME   --source NAME
  --parent <id>        --blocks <id>
  --discovered-from <id>   record that this task was surfaced while doing another
  --json                   print the created/updated task(s) as JSON (id, text, status…)
  add-only:    --note <slug> (link to a note)   --est 90m|2h   --recur weekly|3d
  update-only: --title "new text" (keeps tags/links)  --untag NAME (repeat)
               --project none (clears)  --source none (clears)

NOTE FLAGS (nt note)
  --body TEXT   --tag NAME (repeat)   --source NAME   --json (print as JSON)
  --folder DIR        file under notes/DIR/ (created as needed; or path-style:
                      nt note "decisions/Chose flock over SQLite")
  --field key=value   set extra frontmatter at capture (repeatable, preserved)

LIST/RECALL FLAGS
  --status open|doing|blocked|done   --tag NAME   --project NAME
  --sort urgency|due|created         --all        --json
  --show-blocked                     --source NAME / --since YYYY-MM-DD (recall)

Recurring: add --recur weekly|3d|… ; completing spawns the next occurrence.
Dependencies: add --blocks <id> ; blocked tasks hide unless --show-blocked.

The store lives at $NT_DIR (default ~/.local/share/nt): tasks.txt + notes/*.md.
The TUI follows your terminal's light/dark background; force it with NT_THEME=light|dark.
Optional config: $NT_DIR/config.toml — [defaults] priority/source/agenda_days/editor,
[web] port/host, [tui] theme. Flags and env vars override it.
`
