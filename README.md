# nt

A terminal task & note manager that stores everything as **plain files** —
todo.txt tasks + markdown notes — so your editor, `grep`, `git`, and AI coding
agents can all read and write it directly. Built to be the durable memory layer
for AI sessions: action items an agent creates survive the session in text the
next agent can read back.

![nt — wide split with live detail pane](docs/screenshots/01-tasks-wide.png)

See [SPEC.md](SPEC.md) for the full design.

> **Status: all four phases implemented** — core + CLI + AI loop (P1), the Bubble Tea TUI
> (P2), recurrence + dependencies (P3), and the Claude Code hook + `/nt` skill (P4).

## Install

```bash
# Curl — downloads the latest release binary to ~/.local/bin (no Go needed)
curl -fsSL https://raw.githubusercontent.com/navbytes/nt/main/install.sh | bash

# Go (installs the latest tagged release to $GOBIN)
go install github.com/navbytes/nt@latest

# From source — builds and installs to ~/.local/bin (override with NT_INSTALL_DIR)
git clone https://github.com/navbytes/nt && cd nt && make install
```

Releases are automated: pushing a `vX.Y.Z` tag runs GoReleaser via GitHub Actions
to build the cross-platform binaries + checksums ([RELEASING.md](RELEASING.md)).
Homebrew is planned for later (`brew install navbytes/tap/nt`).

Pure Go, single static binary, no system dependencies. `nt --version` reports the build.

## Build (development)

```bash
go build -o nt .       # Go 1.24+
./nt                   # launch the TUI
./nt help              # CLI reference
make test              # run the test suite
```

## TUI

Run `nt` with no arguments for the interactive terminal UI (Bubble Tea). It
adapts to terminal width — compact strip / standard list / wide split with a
live detail pane — and live-refreshes via fsnotify when another process (a CLI
call, an AI session) writes the store. Three tabs: tasks, notes, and a
**Logbook** of completed work grouped by completion date. Mouse clicks select
rows, activate `[[link]]`/`@tag`/`+project` tokens, and switch tabs. Press `?`
for the full keymap. Essentials:
`j/k` move · `enter` detail · `x` done · `a/A` add · `space`/`V` mark · `X` delete ·
`p` priority · `D` due · `t` tag · `l/L` link/follow · `y` yank · `/` filter ·
`v` group · `.` show done · `1`/`2`/`3` tasks/notes/logbook · `Ctrl+l` lock (read-only) ·
`u` undo · `q` quit.

### Screenshots

|  |  |
|---|---|
| **Tasks** — wide split | **Logbook** — completed work by date |
| ![tasks](docs/screenshots/01-tasks-wide.png) | ![logbook](docs/screenshots/08-logbook-wide.png) |
| **Tasks** — done hidden (`✓ N done` chip) | **Notes** |
| ![done hidden](docs/screenshots/03-tasks-done-hidden.png) | ![notes](docs/screenshots/06-notes.png) |

More views in **[docs/screenshots/](docs/screenshots/)** — regenerate with
`./scripts/screenshots.sh`.

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
To version-control the store, run `nt git-init` (sets up `merge=union` so branches
don't conflict on every add, plus a `.gitignore` for transient files); after a
merge, `nt doctor` reconciles any duplicates.

## Usage

```bash
nt add "fix auth bug" --pri high --due today --tag backend --project api
nt note "JWT expiry" --body "tokens last 24h" --tag auth
nt note "Auth design" --folder work          # file it in notes/work/ (or: nt note "work/Auth design")
nt ready [--source claude] [--json]   # open, unblocked tasks by urgency — start here
nt list [--status open|doing|blocked|done] [--tag T] [--sort urgency] [--all] [--json]
nt done <id|task:N>            # also accepts the 6-char short code shown in list
nt update <id|task:N> --status doing --due tomorrow --pri med
nt add "weekly review" --due monday --recur weekly   # completing spawns the next
nt add "write migration" --blocks task:5             # task:5 hides until this is done
nt list --show-blocked                               # reveal dependency-blocked tasks
nt search "auth" [--tag T…]    # ripgrep + substring, optionally tag-filtered (AND); --tag alone lists
nt tags                        # the tag vocabulary with counts
nt tag <note> +ref -inbox      # retag a note (no $EDITOR; preserves Obsidian frontmatter)
nt links <id|task:N>           # forward links + backlinks (--orphans: notes with none)
nt recall --source claude --json   # read items back — the AI loop
nt log [--since|--days N] [--json]  # completed tasks, newest first (the Logbook)
nt mcp                              # stdio MCP server — typed tools for agents
nt mcp install [--client claude-desktop]   # register nt with an AI client (absolute path, idempotent)
nt web [--port N]                   # browse & read notes in a browser (localhost, mermaid, live-reload)
nt edit <id|task:N>            # safe $EDITOR round-trip (never touches the shared file directly)
nt mv <note> <new-name|folder/path>   # rename/move a note, rewriting all [[links]] to it
nt rm <note> [--force]         # delete a note → .trash/ (refuses if inbound [[links]] would dangle)
nt note "x" --field status=stable     # set arbitrary frontmatter at capture
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

### Notes ↔ Obsidian

**The trend is "use Obsidian as your AI knowledge base." The substance behind it
is plain markdown — not the app.** What actually helps an LLM is the
[Karpathy "LLM wiki" pattern](https://venturebeat.com/data/karpathy-shares-llm-knowledge-base-architecture-that-bypasses-rag-with-an):
a library of plain `.md` the model reads and maintains directly. For a personal
knowledge base that beats RAG on reliability (the model reads the real file, not
an embedding's best guess), token cost (open only what's relevant), and
auditability (`git diff` it, roll it back). nt **is** that pattern, with tasks
and an AI-memory loop on top.

So nt talks to Claude by letting it **read scoped files directly** — `nt recall`,
`nt ready`, and the `nt mcp` typed tools hand back exactly the relevant items.
It deliberately does **not** route the agent through Obsidian's REST-API MCP,
which practitioners find slow, token-heavy, and a whole-vault privacy exposure
that experienced users won't run in their main vault
([forum consensus](https://forum.obsidian.md/t/obsidian-mcp-servers-experiences-and-recommendations/99936)).
Obsidian stays an **optional human GUI** pointed at `notes/` — never a dependency
and never in the agent's path.

nt has no notes GUI of its own — and doesn't need one. Notes are plain `.md` +
YAML frontmatter + `[[wikilinks]]`, so you can **point an Obsidian vault at the
`notes/` folder and use Obsidian as the GUI** while nt owns tasks, the CLI/TUI,
and the AI-memory loop. Folders work both ways: create into one with `nt note "…" --folder work` (or the
path-style `nt note "work/…"`), move with `nt mv`, and nt reads back what Obsidian
writes: nested subfolders,
block-list `tags:`/`aliases:`, notes without an H1 (title falls back to the
filename), and link variants (`[[folder/note]]`, `[[note#heading]]`,
`[[note|alias]]`) resolved by shortest path-suffix — a bare name colliding across
folders is flagged ambiguous rather than guessed. Rename/move is **nt-native**:
`nt mv <note> <new>` (or `r` in the TUI notes tab) renames the file and rewrites
every `[[link]]` to it across tasks and notes, so you don't depend on Obsidian to
keep links intact. (Logseq's outliner model is not a target.)

## Web viewer (`nt web`)

`nt web` starts a **localhost** server to browse and read your notes in a
browser — a folder tree, rendered Markdown with `[[wikilink]]` navigation and
backlinks, full-text search, **Mermaid diagrams**, and light/dark themes that
match the TUI. It also surfaces nt's task↔note graph — **tasks that reference a
note**, backlinks with context snippets, an in-note table of contents, and
`src:claude` provenance — over the same `notes/` files everything else uses.

It's built for moving fast: a **⌘K command palette** to jump to any note,
**search-as-you-type**, a **`/tasks`** dashboard (open work grouped by status,
urgency-sorted), a **`/tags`** browser, a clickable **`/graph`** of your links,
**hover previews** on `[[links]]`, **syntax-highlighted** code, prev/next sibling
navigation, and an adaptive reading width (`⇆`) that keeps prose comfortable while
code and diagrams use the screen. Read-only and localhost-only; live-reloads on
file change. Plain server-rendered HTML + one tiny `app.js` — no framework, no
build, no external requests.

|  |  |
|---|---|
| **Light** — TOC, breadcrumb, tasks & backlinks | **Dark** — Tokyo Night |
| ![nt web — light](docs/screenshots/web-light.png) | ![nt web — dark](docs/screenshots/web-dark.png) |

```bash
nt web              # pick a free port, print the URL (127.0.0.1)
nt web --port 8080  # fixed port
```

It's **read-only** and binds to `127.0.0.1` only — your notes are never exposed
on the network. The page **live-reloads** when a file changes (via fsnotify +
SSE), so editing in your editor, Obsidian, or Zettlr updates the view instantly.
Mermaid is the only vendored asset (embedded gzipped); no external requests, no
CDN — it works fully offline.

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
