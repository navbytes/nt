# nt — Specification

A terminal task & note manager built to be the **durable memory layer for AI coding
sessions**. One binary, plain files, zero config.

Your AI assistant just created three action items — then the session ended and they vanished.
Tomorrow's session starts with no memory of them. `nt` fixes that: it's the place where the
tasks and notes an agent generates **survive the session**, in plain text that the *next*
agent (or you) can read back.

```
nt              ← opens the TUI
nt add "title"  ← adds from anywhere: another terminal, a script, an AI session
nt recall       ← read back what prior sessions captured
```

Because the data is just text on disk, an AI agent, your `$EDITOR`, `grep`, git, and even a
plain todo.txt client all read and write it with zero integration work.

---

## 1. Philosophy

- **Durable AI memory, first.** The reason `nt` exists: action items and notes from AI
  coding sessions persist across sessions and tools. Everything else serves that.
- **One binary, zero config.** Pure Go, single static binary, no system dependencies.
- **Local-first, plain text.** Tasks and notes are human-readable files. No proprietary
  format, no database, no cloud. The files are the source of truth.
- **Interoperable by default.** Plain text means *any* tool reads and writes it — `$EDITOR`,
  `ripgrep`, `git`, todo.txt clients, and AI agents. No schema, no API, no auth to learn.
- **CLI and TUI over the same store.** Anything in the TUI is doable from a script and vice
  versa. They never disagree because they read the same files.
- **AI origin is tracked.** Items carry a `src:` field so you can tell what an agent created
  from what you typed, and an agent can recall its own prior output.

### What changed from the original SQLite design, and why

| | Original README (`nt` v0) | This spec (`nt`) |
|---|---|---|
| Store | Single SQLite file, WAL mode | Plain files: `tasks.txt` + `notes/*.md` |
| Search | SQLite FTS5 | Parse-in-Go for tasks; `ripgrep` for note full-text |
| Concurrency | SQLite WAL (snapshot isolation) | flock + ULID-keyed ops + atomic rename |
| Refresh | 2s poll | `fsnotify` directory watch (sub-second) |
| Interop | Read via the `nt` binary only | Any text tool — *and any AI agent* — reads/writes |

The trade we accept: SQLite gave us snapshot isolation and crash-atomic transactions for
free. Replacing it with files means we must engineer the write/undo/refresh contract
ourselves (§6). In exchange, the data is no longer trapped behind a binary — it is just
text, forever readable, and directly writable by an AI agent without an integration layer.

---

## 2. Design decisions (locked)

1. **Global store**, not project-local. One store at `~/.local/share/nt/`, always available
   regardless of the current directory (override with `NT_DIR`). `nt` is a personal,
   cross-project memory layer — it deliberately does **not** scope to the current repo.
2. **todo.txt-style single file** for tasks (`tasks.txt`), one line per task. Notes are
   separate markdown files.
3. **Files-only search.** `ripgrep` for note full-text; task list/filter/sort parses the
   whole `tasks.txt` into structs in Go (§7.1). No index, no sync layer that can drift.
4. **AI durability is core, not an add-on.** The write→recall loop ships in Phase 1, not as
   a final feature.

> Note: this is a personal global store by design. We do **not** promise "tasks live next to
> your code" — that was the v0 framing and has been removed. Multi-machine sync is a
> git-based, opt-in story (§6.4), not a guarantee of the single shared file.

---

## 3. On-disk layout

```
~/.local/share/nt/            # $NT_DIR
├── tasks.txt                 # active tasks — todo.txt format, one line per task
├── done.txt                  # archived completed tasks (via `nt archive`)
├── undo.jsonl                # append-only undo transaction journal (§6.3)
├── views.json                # saved smart views — `nt view save/recall` (optional)
├── web.pid / web.log         # a backgrounded `nt web --detach` server + its output (optional)
├── tasks.txt.lock            # advisory lock file (§6.4)
├── notes/
│   ├── jwt-token-lifetime.md
│   └── 2026-06-05-standup.md
└── nt.log                    # JSON logs, rotated at 10MB
```

The directory is created on first run. `tasks.txt` and `notes/` are independent and can be
`cat`/`grep`/`git init`/opened in any editor without `nt` running.

---

## 4. Task format (todo.txt + conventions)

Each task is one line in [todo.txt](https://github.com/todotxt/todo.txt) format. `nt`'s
richer metadata layers onto todo.txt's native pieces plus its `key:value` extension
mechanism and widely-used community conventions (Simpletask / topydo).

```
(A) fix auth bug +api @backend due:2026-06-05 [[jwt-token-lifetime]] src:claude id:01JZ8RT3
x 2026-06-05 2026-06-01 write migration +api parent:01JZ8RT3 id:01JZ8RT9
```

### Field mapping

| nt concept        | todo.txt encoding                    | Notes |
|-------------------|--------------------------------------|-------|
| open / done       | leading `x ` + completion date       | Native todo.txt |
| priority h/m/l    | `(A)` / `(B)` / `(C)`                | Native todo.txt |
| state doing/blocked | `s:doing` / `s:blocked`            | No native non-binary state |
| creation date     | `YYYY-MM-DD` after priority          | Native todo.txt |
| completion date   | `YYYY-MM-DD` right after `x`          | Native todo.txt |
| project           | `+project`                           | Native; one per task |
| tags              | `@tag` (repeatable)                  | Native contexts, used as tags |
| due date          | `due:YYYY-MM-DD`                      | De-facto standard key |
| start / defer     | `t:YYYY-MM-DD`                        | todo.txt "threshold": not actionable until this date; agenda/ready hide future-start |
| recurrence        | `rec:3d`/`rec:weekly` (from completion); `rec:+3d` (strict: anchored to due, rolls forward) | Completing spawns the next; strict never spawns overdue |
| subtask           | `parent:<id>`                        | Typed task→task; one level only |
| dependency        | `blocks:<id>`                        | Typed task→task; hides blocked |
| provenance        | `discovered:<id>`                    | Work surfaced while doing another task (AI memory) |
| link (untyped)    | `[[note-slug]]` / `[[<ULID>]]`       | Cross-link to any note or task (§5.1) |
| AI / origin       | `src:claude`                         | Defaults to `src:cli` / `src:tui` |
| stable id         | `id:<ULID>`                          | Primary handle (§7.2) |

### Format rules (binding on the parser/writer)

- **Status precedence:** a leading `x` always means done. `s:doing` / `s:blocked` apply only
  to not-yet-done tasks.
- **Completed-task priority:** when a task is completed, its `(A)` priority is preserved as a
  `pri:A` key (Simpletask convention), not dropped. Re-opening restores `(A)`.
- **Lossless round-trip (required).** The file is modeled as an **ordered list of nodes**
  (each node is a parsed task *or* a preserved raw line / blank line). Each task node retains
  its original token order and **any `key:value` tokens `nt` does not model** (e.g. another
  client's `t:` threshold). The writer mutates only the tokens it owns and re-emits the rest
  verbatim. A parse→write of an unmodified line MUST be byte-identical (enforced by test).
- **No comment syntax.** todo.txt has none; `nt` does not invent one. Blank lines and line
  order are preserved; arbitrary `# text` is treated as a task titled "# text", same as any
  todo.txt client.
- **IDs are [ULID](https://github.com/ulid/spec)s** — sortable by creation time, stable for
  the task's life. Every task `nt` writes gets an `id:`. Hand-added lines without an `id:`
  are assigned one on the next mutation that touches them.
- **Referential integrity:** `parent:`/`blocks:` reference ULIDs. `nt archive` of a parent
  whose children are still open is refused (or warns); see §9.
- **Links are plain text.** A `[[…]]` token in a task line is just text to other todo.txt
  clients (round-trip-safe); `nt` resolves and renders it per §5.1. A task may carry any
  number of `[[…]]` links.

---

## 5. Note format

Markdown files in `notes/` with light YAML frontmatter:

```markdown
---
id: 01JZ8RTQ2K
tags: [auth, backend]
source: claude
created: 2026-06-05T10:00:00Z
---

# JWT token lifetime

Tokens expire after 24h; refresh window is 7d. See [[oauth-flow]].
```

- Frontmatter is optional metadata; the body is free markdown (Glamour-rendered in the TUI,
  editable with `$EDITOR`).
- Filename is a slug of the title (or a datetime when untitled, à la `nb`).
- Notes may live in **subfolders** of `notes/`. Create into one with
  `nt note "…" --folder work/auth` (or path-style `nt note "work/auth/…"`); the
  folder is created as needed, and folders that would escape `notes/` (absolute or
  `..`) are refused. `List` recurses the tree, and `[[bare-name]]` links resolve
  across folders by shortest path-suffix (§5.1), so foldering never breaks links.
- `[[…]]` links resolve to a **note** (by filename, title, or `id:`) or a **task** (by
  ULID / short prefix) — see §5.1. Tags come from frontmatter `tags:` (inline body `#tags`
  are not parsed).
- Notes are one file each, so editing a note directly in `$EDITOR` is safe (atomic save, no
  shared-file lock needed).

**Obsidian-compatible (use Obsidian as the notes GUI).** Point an Obsidian vault at `notes/`
and it works both ways — nt already writes plain `.md` + YAML frontmatter + `[[wikilinks]]`
Obsidian reads, and nt *reads* Obsidian's conventions: notes are discovered **recursively**
through subfolders (`.obsidian/`, hidden dirs, and non-`.md` files skipped); frontmatter
`tags`/`aliases` parse in inline, bare-comma, and **YAML block-list** forms (plus the
deprecated singular `tag:`); a missing H1 falls back to a `title:`/`aliases:` value or the
filename; and links resolve by **shortest path-suffix** like Obsidian — `[[note]]`,
`[[folder/note]]`, `[[note#heading]]`, `[[note|alias]]`, `[[note.md]]` all resolve, a bare
name that collides across folders is reported as **ambiguous** (qualify it with a folder)
rather than silently guessed. **Renaming is nt-native** — `nt mv <note> <new>` (or `r` on the
TUI notes tab) renames/moves the file and rewrites every `[[link]]` to it across tasks and
notes (preserving each link's folder/`#fragment`/`|alias`), so you don't depend on Obsidian to
keep links intact. A pure folder move needs no rewrite (resolution is path-suffix); a rename
that would collide with another note's name is refused. Out of scope: inline `#tags`, embeds
`![[…]]`, block-ref navigation, Logseq's outliner model.

---

## 5.1 Linking & backlinks (cross-item)

A unified model so tasks and notes reference each other in every direction. It reuses two
things Phase 1 already builds — the universal `id:<ULID>` handle and the ripgrep search
engine — so it adds almost no new machinery.

**Forward links — one syntax, works in task lines and note bodies:**

| From → To | Example |
|---|---|
| task → note | `(A) fix auth bug [[jwt-token-lifetime]] id:01JZ8RT3` |
| note → task | `Root-caused in [[01JZ8RT3]].` (in a note body) |
| note → note | `See [[oauth-flow]].` |
| task → task (typed) | `parent:<id>` / `blocks:<id>` — carry semantics `[[…]]` doesn't |

- **Resolution order** for `[[target]]`: exact note slug → note title → note `id:` → task
  ULID (full or unambiguous short prefix). Ambiguous/unresolved links render as a dim
  "broken link" but are never rewritten or dropped.
- `parent:`/`blocks:` remain **typed** task→task links (they drive rollups and blocked-hiding).
  `[[…]]` is the **untyped "see also"** link that spans tasks *and* notes.

**Backlinks are a query, not stored state.** Because the store is files-only with no index
(§2.3), "what links to this item?" is just a ripgrep for the target's `id:`/slug across
`tasks.txt` + `notes/`, computed on demand. Nothing to maintain, nothing to drift — it falls
straight out of §7.1.

- CLI: `nt links <id>` prints forward links *and* backlinks for an item.
- TUI: the detail overlay shows a **"Linked from"** section (§12).

---

## 6. The write / undo / refresh contract

This is the heart of the design — the part SQLite handled for us and we now own. Treat every
rule here as binding.

### 6.1 ULID-keyed mutation engine (no whole-file overwrites)

A single mutation engine, shared by CLI and TUI, is the **only** thing that writes
`tasks.txt`. Every mutation is an operation addressed by ULID, never a dump of in-memory
state:

- `add(line)` — append a new task node.
- `set(id, field, value)` — locate the node by ULID, change one field, preserve all other
  tokens.
- `delete(id)` / `archive(id…)` — remove/move nodes by ULID.

Apply path, under the lock (§6.4): **re-read the file → locate node(s) by ULID → apply op →
atomic write (temp + `rename(2)`)**. If a target ULID is absent (archived/deleted by another
writer), the op **fails loudly** — it never resurrects a removed task.

The **TUI holds its `[]Task` as a read-only view.** "Saving" an edit emits `set(...)` ops and
then reloads from disk; it never writes its whole list back. This closes the lost-update hole
where a concurrent `nt add` from an AI session would otherwise be silently destroyed.

### 6.2 `$EDITOR` never touches the shared file

`nt edit task:<id>` must **not** open the real `tasks.txt` (an editor's save-via-rename
bypasses the lock and races every other writer). Instead: extract the task's line → write it
to a temp file → open `$EDITOR` on the temp → on exit, re-apply the result as a `set` op
under the lock. Editing a **note** file directly is fine (one file per note).

### 6.3 Undo journal = append-only transactions

`undo.jsonl` is an append-only log of **transactions**, not single entries. Each forward
mutation writes, atomically and under the same lock, a transaction record:

```json
{"ops": [<ulid-keyed inverse ops>], "before": [<raw before-images of touched lines>], "ts": "..."}
```

- Inverse ops are **keyed by ULID**, never positional `task:N`.
- A transaction can hold **multiple** inverse ops — `archive` (multi-line, two-file) and
  recurrence-spawn (un-complete original + delete spawned child) are each one transaction
  with several ops. The "single inverse entry" idea from the first draft is dropped.
- `nt undo` pops the last transaction, **validates** that current state matches the recorded
  post-image (by ULID), applies the inverses under the lock, and records its own forward
  transaction (so undo-of-undo / redo is sane). If the world moved underneath, it refuses
  with a clear message rather than corrupting state.
- Ordering across processes: the journal append happens **under the `tasks.txt` lock**, in
  the same critical section as the mutation, so "last" is well-defined. Write the journal
  entry **before** the forward mutation so a crash can't leave a mutation without its inverse.

### 6.4 Locking & sync honesty

- **BSD `flock` on `tasks.txt.lock`** (a separate file, so the rename that replaces
  `tasks.txt` can't swap the inode out from under a held lock). Bounded acquisition wait
  (~2s) then a clear "store is busy" error — never a silent no-op.
- **Local filesystem only.** `flock` is unreliable/absent on NFS. The store must live on a
  local FS for concurrent access to be safe; this is documented loudly.
- **Multi-machine sync is last-writer-wins.** Dropbox/iCloud/Syncthing do their own
  atomic-rename and conflict-copy creation; `flock` means nothing to them. Honest story:
  single-machine concurrency is locked and safe; cross-machine is **git-based** or
  last-writer-wins with possible conflict files. We do not advertise "just sync the
  file" as safe.
- **Git-tracked stores (opt-in).** `nt git-init` drops a `.gitattributes`
  (`tasks.txt`/`done.txt` `merge=union`) so concurrent branches don't conflict on every
  append — git keeps both sides' lines instead of emitting markers — plus a `.gitignore` for
  local/transient files (`undo.jsonl`, `tasks.txt.lock`, `nt.log`, `.claude-sync.json`), and
  `git init`s the store. Union merges can leave duplicate-ULID lines, so **`nt doctor`**
  reconciles after a merge: it drops duplicate ids (keeping a completed line over an open one)
  and assigns ids to any id-less line, under the lock. Not journaled — git is the recovery
  path; `nt doctor --check` is a non-mutating dry run (exit 1 if issues) for pre-commit/CI.
  This keeps the single greppable `tasks.txt`; per-task file sharding is deferred (§15).

### 6.5 fsnotify refresh

- **Watch the directory `$NT_DIR`, not the file.** Our own atomic rename (and any editor's
  save-via-rename) replaces the `tasks.txt` inode, which would silently kill a file-level
  inotify watch. Directory-watch also catches note create/delete. Re-stat after each event
  rather than trusting the event payload (portable across Linux inotify and macOS kqueue).
- **Debounce** events ~50–100ms (one logical save emits several events).
- **Ignore self-writes** (compare mtime+size, or set an "ignore next event" flag for writes
  we originated) to avoid reload loops and flicker.

---

## 7. CLI & search

### 7.1 Search vs. filter are different jobs

- **Note full-text search → `ripgrep`** across `notes/*.md` (built-in walker fallback when
  `rg` is absent). `nt search "race condition" --type note`.
- **Literal task-text search → `ripgrep`** over `tasks.txt` is fine as a *prefilter*, but
  each hit is then parsed back into a Task for display.
- **Task list / filter / sort is NOT a grep job.** `--status open`, `--tag bug`,
  `--project api`, `--sort urgency`, hide-blocked all require parsing the whole `tasks.txt`
  into `[]Task` and operating on structs (e.g. `@bug` must not match `@bugfix`; "open" is the
  absence of `x` and unblocked deps; urgency is a computed score). At one user's volume
  (hundreds–low thousands of lines) a full parse per invocation is <1ms. ripgrep is **not** in
  the structured-query path.
- **Backlinks → `ripgrep`.** "What links to item X?" is a ripgrep for X's `id:`/slug across
  `tasks.txt` + `notes/` (§5.1). Like note search, it's a grep job — no stored backlink index.

### 7.2 Item handles: ULID-first

- The **primary handle is the ULID** (full or unambiguous short prefix). CLI mutating
  commands and the AI hook always round-trip ULIDs.
- `task:N` is a **positional convenience for interactive use only**, resolved against the
  current file at execution time and explicitly best-effort (a concurrent archive can shift
  what `task:3` means between two commands). `nt list` prints short ULID prefixes so
  scripts/agents can capture a stable handle.
- **Enforced:** `task:N` / bare `N` is *refused for non-interactive callers* — if stdin or
  stdout isn't a TTY (an agent or script), the command errors and tells the caller to use the
  id. This makes the read-then-act footgun impossible for agents while keeping the shortcut
  for humans.

### 7.3 Commands

```bash
nt                                   # launch the TUI
nt add "fix auth bug" --pri high --due today --tag backend --project api [--source claude]
nt note "JWT expiry" --body "..." --tag auth [--folder work] [--source claude]
nt list [--status open] [--tag bug] [--project api] [--sort urgency] [--json]   # (ls)
nt recall [--source claude] [--since 2026-06-01] [--json]   # read back prior items (AI loop)
nt log [--since 2026-06-01] [--days 7] [--source claude] [--json]   # completed tasks, newest first
nt done <id|task:N>                  # mark done  (do)
nt update <id|task:N> --status doing --pri med --due +3d     # (up)
nt search "race condition" [--type note|task]                # (q)
nt links <id|task:N> [--json]        # forward links + backlinks for an item (§5.1)
nt archive                           # move done tasks → done.txt
nt undo                              # revert the last transaction
nt edit <id|task:N> | nt edit note:<slug>   # safe edit via temp file (§6.2)
nt mv <note> <new-name|folder/path>  # rename/move a note, rewriting all [[links]] to it
nt path                              # print $NT_DIR
```

- `--json` on `list`/`recall` emits machine-readable output (stable schema, ULIDs) so AI
  agents parse reliably instead of scraping rendered text.
- Date parsing: `today`, `tomorrow`, weekday names (= next occurrence), `+3d`, `YYYY-MM-DD`.

---

## 8. AI session durability (core)

This is the product's reason to exist, so the **write→recall loop is Phase 1**, and the loop
is closed — agents can both write and reliably read back their own prior output.

```bash
# during a session, an agent captures work
nt add "refactor auth middleware" --source claude --tag backend
nt note "discovered race condition in token refresh" --source claude

# a later session recalls what was captured, as structured data
nt recall --source claude --json
```

- **Stable, machine-readable contract:** appending a todo.txt line to `tasks.txt`, or calling
  `nt add`, with `--json` recall back out. No MCP server, no schema, no auth.
- **`src:`** distinguishes AI-created items; the TUI badges them.
- **Claude Code polish (Phase 4):** a PostToolUse hook mirroring `TaskCreate`/`TaskUpdate`
  into `nt add`/`nt update`, and a `/nt` skill — built on the Phase 1 loop, not inventing it.

---

## 9. Features

- **Subtasks** — `--parent task:1`, one level deep, `s` expands inline with a `[done/total]`
  rollup. Archive of a parent with open children is refused (keeps the rollup honest).
- **Full-text search** — ripgrep over notes; structured filter/sort over tasks (§7.1).
- **Linking & backlinks** — `[[…]]` cross-links any task or note in any direction; backlinks
  ("Linked from") computed on demand via ripgrep, no index (§5.1).
- **Multi-select & bulk ops** (TUI) — `space`/`V` mark tasks (ULID-keyed, survive
  regroup/filter); `x`/`p`/`D`/`t`/`X` act on the whole set in one undo transaction;
  destructive bulk ops (done-with-recurrence, delete) confirm first.
- **Delete** — `X` (TUI) / `nt rm <id…>` (CLI), journaled so `nt undo` restores.
- **Logbook** — completed tasks as a work-journal: a TUI tab (`3`) grouped by completion
  date (newest first, with the source of each task), and `nt log` on the CLI — both read the
  same domain rule (`task.CompletedSince`). The tasks list stays clean; a header `✓ N done`
  chip keeps hidden-done visible. Doubles as the AI recall feed (`nt log --json`).
- **Token activation** — `f` (keyboard follow) or mouse-click a `[[link]]`/`@tag`/`+project`
  to navigate or scope/regroup; **yank** (`y`) copies id/line/text to the clipboard.
- **Flexible metadata** — tags, project, due, priority, links; use what helps, skip the rest.
- **Recurring tasks** — `rec:weekly` / `rec:3d`; completing one spawns the next occurrence
  (advancing the due date) in the same undo transaction.
- **Dependencies** — `--blocks task:5`; blocked tasks hide from the default list and show ⊘,
  `nt list --show-blocked` (CLI) / `b` (TUI) reveals them.

---

## 10. Onboarding & install

- **First run** creates `$NT_DIR`, seeds one example task + note, and prints the three
  commands that matter (`nt add`, `nt`, `nt recall`). No config, no account.
- **`tasks.txt` header**: a leading blank-safe hint line documenting the `key:value`
  conventions so hand-editors aren't lost (kept compatible — not a todo.txt comment).
- **Install** should offer a plain release binary / `brew` tap in addition to any
  build-from-source path; a `gh api | base64 | bash` one-liner is an adoption blocker.

---

## 11. Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `NT_DIR` | Store directory | `~/.local/share/nt` |
| `EDITOR` | Editor for `nt edit` / body editing | `vi` |
| `NT_ICONS` | `nerd` for Nerd Font icons | standard Unicode |
| `NT_GIT` | `1` to auto-commit each change (multi-machine history) | off |

Everything is plain text under `$NT_DIR`. Back it up or `git init` it. For multi-machine use,
prefer `NT_GIT=1` over file-syncing the store (§6.4).

---

## 12. TUI

Bubble Tea + Lipgloss, Glamour for note bodies. `fsnotify` directory-watch live refresh
(§6.5). TUI state is a read-only view; edits emit ULID-keyed ops (§6.1).

### Layout adapts to terminal width (kept — moderate-trim retains all three)
- **60+ cols** — task list grouped by date / project / tag, priority + due columns.
- **25–40 cols** — compact monitoring strip for a side pane.
- **120+ cols** — split list + detail.

The **detail overlay** renders the item's forward `[[…]]` links and a **"Linked from"**
backlinks section (both from §5.1); `L` opens a picker to jump to any linked item.

### Essential keybindings (full list under `?`)
Edit keys act on the **marked set** if any, else the current task.

| Key | Action | • | Key | Action |
|-----|--------|---|-----|--------|
| `j`/`k`, `↑`/`↓` | navigate | | `t`/`T` | add / remove tag |
| `Ctrl+d`/`Ctrl+u` | half-page scroll | | `D` | set due date |
| `g`/`G` | top / bottom | | `p` | priority (cycle; absolute when marked) |
| `Enter` | focus detail (j/k scroll body) | | `l`/`L` | add link / follow link |
| `x` (or `dd`) | toggle done | | `f` | follow: label a token to activate |
| `X` | delete (confirms; `u` undoes) | | `/` | filter (searches note bodies on notes) |
| `space` / `V` | mark / visual range-select | | `v` | cycle grouping |
| `y` | yank → `y` id · `l` line · `t` text | | `.` / `b` | show-hide done / blocked |
| `a`/`A` `r` `e`/`E` | add / rename / edit | | `u` | undo (again = redo) |
| `1`/`2`/`3`/`tab` | tasks / notes / logbook | | `‹`/`›` | resize the list/detail split |
| `Ctrl+l` | lock (read-only: blocks writes) | | | |

The **footer keybar is contextual** — it shows only the keys that apply to the
current tab and selection (e.g. no due/tag on notes, `x reopen` in the logbook,
`f follow` only when the selected item has tokens).

**Mouse** (on by default; `NT_MOUSE=0` disables, Shift-drag = native selection):
wheel scrolls, click selects a row, click a `[[link]]`/`@tag`/`+project` activates it,
click a tab to switch, and drag the list/detail divider to resize it.

### 12.1 Web GUI (`nt web`)

A localhost HTTP adapter (`internal/web`) — the browser GUI over the same
`task`/`note`/`links` domain as the CLI/TUI/MCP. Binds `127.0.0.1` only (no
network exposure, no auth). Read-only by default; `nt web --edit` enables writes
(notes **and** tasks), each guarded by a per-process CSRF token sent as a custom
header (cross-site POSTs fail the CORS preflight).

**Read model.** Every request serves from an in-memory snapshot (`readmodel.go`)
that parses the store once and precomputes the link graph — note→note backlinks,
the task→note reference panel, forward adjacency, and the orphan set, keyed by
note path. A note page is a map lookup, not a per-request walk + ripgrep. The
snapshot is rebuilt from a **debounced, self-write-aware** fsnotify watcher (SPEC
§6.5): an atomic-rename event burst collapses into one rebuild, transient files
(`.nt-*.tmp`, lock, undo, log) are ignored, and the adapter's own writes don't
bounce the editing client. With no watcher (tests/embedders) reads build fresh.

**Browse + read.** Folder tree, Markdown via **goldmark** (in the module graph
via Glamour), `[[wikilink]]` navigation (resolved → `/n/<id>` stable handles;
unresolved/ambiguous → a "did you mean" page), backlinks, full-text search,
command palette (⌘K), hover previews, **Mermaid** diagrams (vendored gzipped,
client-side, offline). Light/dark Tokyo Night themes.

**Interactive tasks** (`--edit`). The `/tasks` dashboard can complete / reopen /
set-status / delete / add — every action routed through `mutate.Engine.Apply`
(lock + re-read + undo journal), so a browser write gets the same safety as a
CLI/agent write and concurrent human+agent edits can't clobber each other. New
tasks are stamped `src:web`. UI is **htmx** fragment swaps (vendored, no build);
a write broadcasts a typed `tasks` SSE event so other open clients refresh just
the list.

**Editing notes** (`--edit`). A split live-preview editor: raw buffer on the
left, a preview on the right that re-renders through the server's own
`renderBody` (`POST /preview`) so it matches what a save produces. Saves are
atomic, frontmatter preserved verbatim, and guarded against lost updates — the
editor sends the file's `ETag` as `If-Match`; a concurrent write yields **409**
(reload to merge) instead of a silent clobber.

**Graph.** `/graph` is an interactive force-directed canvas (`graph.js`, no new
vendored lib): pan/zoom/drag, hover-highlights a node's neighborhood,
click-to-open, live filter, and color by folder/source. The server emits the
graph as JSON from the snapshot adjacency; the client runs the layout.

**Live updates** via fsnotify + SSE, now typed (`reload` = full reload for
external changes; finer kinds drive surgical fragment refresh). The whole
adapter is embeddable — `Server.Handler()` lets the Wails desktop shell render
the identical UI in a native window (see `desktop/`, ADR 0001).

---

## 13. Tech stack

- **Go 1.24+**, single static binary, no CGo.
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lipgloss](https://github.com/charmbracelet/lipgloss) — TUI.
- [Glamour](https://github.com/charmbracelet/glamour) — markdown rendering.
- [fsnotify](https://github.com/fsnotify/fsnotify) — directory-watch refresh.
- [goldmark](https://github.com/yuin/goldmark) + a frontmatter parser — notes.
- `ripgrep` (external, optional) — note full-text search; built-in walker fallback.

---

## 14. Build phases (AI-core, moderate trim)

**Phase 1 — Core + CLI + AI loop** *(the hard phase: it owns the write/undo/refresh contract)*
- Store layout; ordered-node todo.txt parser/writer with **lossless round-trip** (§4) and a
  parse→write byte-identical test.
- **ULID-keyed mutation engine** + flock + atomic rename (§6.1, §6.4).
- **Undo transaction journal** (§6.3).
- CLI: `add / list / done / update / note / search / links / archive / undo / edit / path` —
  with ULID-first handles (§7.2), structured task filter/sort incl. `--sort urgency`, ripgrep
  note search (§7.1), and `--json` output.
- **Linking & backlinks** (§5.1): `[[…]]` resolution across tasks + notes, `nt links <id>`
  forward + ripgrep backlinks.
- **AI loop:** `--source`, `nt recall`, machine-readable `--json` (§8).
- First-run onboarding (§10).

**Phase 2 — TUI**
- Bubble Tea list, **fsnotify directory-watch** with debounce + self-write suppression (§6.5),
  detail overlay, the responsive layouts, keybindings, edit-via-temp-file (§6.2).

**Phase 3 — Recurrence & dependencies** ✓ *implemented*
- `rec:` spawn-on-done (one multi-op undo transaction, §6.3); `blocks:` dependencies with
  blocked-hiding (`--show-blocked` / TUI `b`); `EffectiveStatus` surfaces dependency-blocked
  tasks as ⊘ in both CLI and TUI.

**Phase 4 — Claude Code polish** ✓ *implemented*
- `nt hook` (PostToolUse) mirrors `TodoWrite` into the store idempotently (per-session
  todo→ULID map, status-mapped, `src:claude`); the bundled `/nt` skill teaches Claude to
  capture and `nt recall`. Setup: docs/claude-integration.md.
- `nt mcp` runs a stdio **MCP server** (newline-delimited JSON-RPC 2.0, no SDK dep) exposing
  typed tools — `nt_ready`/`nt_add`/`nt_done`/`nt_update`/`nt_note`/`nt_recall`/`nt_log` —
  for MCP clients. A thin driving adapter over the same engine/domain as the CLI and TUI;
  defaults `source` to `claude` and refuses positional handles.

---

## 15. Open questions (non-blocking)

- **`done.txt` vs inline `x`:** keep completed tasks inline until `nt archive` (chosen —
  keeps undo of `done` a one-line in-place edit; only `archive` is cross-file).
- **`nt.log` rotation under concurrent writers:** use an append-only logger and rotate under a
  separate lock (concurrent rotation is its own lost-update problem).
- **Short-ULID collision policy** in `nt list` output: widen the prefix until unambiguous.

---

*Status: **all four phases implemented** — core + CLI + AI loop + linking (P1), Bubble Tea
TUI (P2), recurrence + dependencies (P3), Claude Code hook + /nt skill (P4). Global store,
todo.txt tasks, files-only search, AI-durability core, unified linking + backlinks (§5.1).
Engineering review folded in (ULID-op write model, transactional undo, lossless round-trip,
local-FS locking, parse-for-filter). See README.md and docs/claude-integration.md for usage.*
