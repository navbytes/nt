# nt

A terminal task & note manager that stores everything as **plain files** —
todo.txt tasks + markdown notes — so your editor, `grep`, `git`, and AI coding
agents can all read and write it directly. Built to be the durable memory layer
for AI sessions: action items an agent creates survive the session in text the
next agent can read back.

See [SPEC.md](SPEC.md) for the full design.

> **Status: all four phases implemented** — core + CLI + AI loop (P1), the Bubble Tea TUI
> (P2), recurrence + dependencies (P3), and the Claude Code hook + `/nt` skill (P4).

## Install

```bash
# Homebrew (once the tap is published)
brew install navbytes/tap/nt

# Go (installs the latest tagged release to $GOBIN)
go install github.com/navbytes/nt@latest

# From source — builds and installs to ~/.local/bin (override with NT_INSTALL_DIR)
git clone https://github.com/navbytes/nt && cd nt && make install
# or: ./install.sh
```

Pure Go, single static binary, no system dependencies. `nt --version` reports the build.

## Build (development)

```bash
go build -o nt .       # Go 1.22+
./nt                   # launch the TUI
./nt help              # CLI reference
make test              # run the test suite
```

## TUI

Run `nt` with no arguments for the interactive terminal UI (Bubble Tea). It
adapts to terminal width — compact strip / standard list / wide split with a
live detail pane — and live-refreshes via fsnotify when another process (a CLI
call, an AI session) writes the store. Press `?` for the full keymap. Essentials:
`j/k` move · `enter` detail · `dd` done · `a/A` add · `e` edit · `p` priority ·
`D` due · `t` tag · `l/L` link/follow · `/` filter · `v` group · `u` undo · `q` quit.
See [docs/tui-mockup.html](docs/tui-mockup.html) for the visual design.

## Store

One global store at `$NT_DIR` (default `~/.local/share/nt`):

```
~/.local/share/nt/
├── tasks.txt     # todo.txt format, one line per task
├── done.txt      # archived completed tasks
├── undo.jsonl    # undo transaction journal
└── notes/*.md    # markdown notes with YAML frontmatter
```

Everything is plain text — open it in any editor, `grep` it, or `git init` it.

## Usage

```bash
nt add "fix auth bug" --pri high --due today --tag backend --project api
nt note "JWT expiry" --body "tokens last 24h" --tag auth
nt list [--status open|doing|blocked|done] [--tag T] [--sort urgency] [--all] [--json]
nt done <id|task:N>            # also accepts the 6-char short code shown in list
nt update <id|task:N> --status doing --due tomorrow --pri med
nt add "weekly review" --due monday --recur weekly   # completing spawns the next
nt add "write migration" --blocks task:5             # task:5 hides until this is done
nt list --show-blocked                               # reveal dependency-blocked tasks
nt search "auth"               # ripgrep over notes + substring over tasks
nt links <id|task:N>           # forward links + backlinks (both directions)
nt recall --source claude --json   # read items back — the AI loop
nt edit <id|task:N>            # safe $EDITOR round-trip (never touches the shared file directly)
nt archive                     # move done tasks to done.txt
nt undo                        # revert the last change (and undo-again to redo)
nt path                        # print $NT_DIR
```

### Task format (todo.txt + conventions)

```
(A) fix auth bug +api @backend due:2026-06-05 [[jwt-expiry]] src:claude id:01JZ8RT3
```

- `(A)/(B)/(C)` priority · `+project` · `@tag` · `due:` · `src:` origin · `id:` ULID
- `[[target]]` links to any note or task; `parent:`/`blocks:` are typed task links
- Completing a task preserves its priority as `pri:A`; reopening restores `(A)`

### Linking

`[[ ]]` cross-links tasks and notes in any direction. Backlinks ("linked from")
are computed on demand by scanning files — no index to maintain.

## Claude Code integration

`nt` is built to be the durable memory layer for AI sessions. Two integration points:

- **PostToolUse hook** — `nt hook` mirrors Claude's `TodoWrite` list into the store
  (idempotent, `src:claude`, status-mapped). Wire it in `~/.claude/settings.json`.
- **`/nt` skill** — teaches Claude to capture tasks/notes and `nt recall` prior context.

Setup and walkthrough: **[docs/claude-integration.md](docs/claude-integration.md)**.

```bash
# during a session (hook does this automatically, or call it directly):
nt add "fix token refresh race" --source claude --tag auth
# next session — read it back:
nt recall --source claude --json
```

## What's guaranteed (the hard parts)

- **Lossless round-trip:** an unmodified `tasks.txt` line is re-emitted
  byte-for-byte, preserving unknown `key:value` tokens from other todo.txt tools
  (enforced by test).
- **No lost updates:** every write goes through one ULID-keyed mutation engine
  that locks, re-reads, mutates, and atomically renames — so a concurrent
  `nt add` from an AI session is never clobbered (concurrency test included).
- **Transactional undo:** each change journals before-images keyed by ULID;
  `nt undo` reverses them and supports undo-again-to-redo.

## Tests

```bash
go test ./...
```

## License

MIT
