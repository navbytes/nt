package cli

import (
	"flag"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/dateparse"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
	"github.com/navbytes/nt/internal/workstream"
)

// shortDate trims an RFC3339-ish timestamp to its YYYY-MM-DD prefix.
func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// noteFolder is the directory a note lives in, relative to notes/ ("" = root).
func noteFolder(n *note.Note) string {
	return path.Dir(n.Rel)
}

type indexNote struct {
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Folder      string   `json:"folder,omitempty"`
	Source      string   `json:"source,omitempty"` // author/agent that captured it — ownership on a shared store
	Updated     string   `json:"updated,omitempty"`
	Tier        string   `json:"tier,omitempty"` // "pinned"|"recent" on a tiered catalog
}

// cmdIndex prints a compact catalog of the knowledge base — one line per note
// (id · title · one-line description · tags · folder) plus the active task list,
// and NO note bodies. This is the always-in-context "index" of the progressive-
// disclosure pattern: an agent loads it cheaply at session start, then fetches
// only the notes it needs by id (nt show / nt_get) or nt search. It replaces the
// old bulk `recall` dump, which grew linearly with the whole corpus.
//
//	nt index                 # md catalog: notes + open tasks
//	nt index --json          # structured
//	nt index --tag auth      # scope to a tag (AND, repeatable)
//	nt index --folder ref    # scope to a folder
//	nt index --project foo   # scope to a project (hard filter, unlike `recall --project`)
func cmdIndex(args []string) int {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	folder := fs.String("folder", "", `only notes under this folder, e.g. ref ("." = root notes)`)
	project := fs.String("project", "", `only notes/tasks in this project — a hard filter on the note's "project:" frontmatter and a task's +project tag (unlike "recall --project", which only ranks, never excludes)`)
	asJSON := fs.Bool("json", false, "machine-readable output")
	noTasks := fs.Bool("no-tasks", false, "omit the active-task section")
	all := fs.Bool("all", false, "full catalog: every note stub, no tiering (large stores tier by default)")
	limit := fs.Int("limit", 0, "cap the note catalog to N (0 = all); scope with --tag/--folder for large stores")
	updatedSince := fs.String("updated-since", "", "only notes changed on/after this date (14d = last 14 days | today | YYYY-MM-DD) — 'what's new since last session'")
	sinceAlias := fs.String("since", "", "alias for --updated-since (matches 'nt log --since')")
	ws := fs.String("workstream", "", `scope to a workstream (default: NT_WORKSTREAM; "*" = all)`)
	var tags stringSlice
	fs.Var(&tags, "tag", "only notes with this tag (repeatable, AND)")
	flags, _ := splitArgs(args, map[string]bool{"json": true, "no-tasks": true, "all": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if *updatedSince == "" {
		*updatedSince = *sinceAlias
	}
	since := ""
	if s := strings.TrimSpace(*updatedSince); s != "" {
		d, ok := dateparse.PastDate(s)
		if !ok {
			return usageErr(fmt.Errorf("index: --updated-since: unrecognized date %q (try 14d = 14 days ago, today, or YYYY-MM-DD)", s))
		}
		if d > time.Now().Format("2006-01-02") {
			// A future cutoff silently matches nothing — the classic "+14d means
			// last 14 days, right?" trap. Say so instead of returning [].
			fmt.Fprintf(os.Stderr, "index: --updated-since %s resolves to a FUTURE date (%s) — every note predates it; for \"the last 14 days\" use --updated-since 14d\n", s, d)
		}
		since = d
	}
	e, ok := engine()
	if !ok {
		return 1
	}

	notes := note.Active(mustNotes(e))
	prefix := strings.Trim(*folder, "/")
	rootOnly := prefix == "." // `--folder .` expands the "(root)" rollup line
	var filtered []*note.Note
	folderSeen := map[string]bool{}
	for _, n := range notes {
		if n.Reserved() {
			continue // task-detail notes aren't part of the KB catalog
		}
		folderSeen[noteFolder(n)] = true
		if rootOnly && strings.ContainsRune(n.Rel, '/') {
			continue
		}
		if !rootOnly && prefix != "" && !strings.HasPrefix(n.Rel, prefix+"/") {
			continue
		}
		match := true
		for _, want := range tags {
			if !contains(n.Tags, want) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if *project != "" && n.Project() != *project {
			continue
		}
		if since != "" && n.ChangedDate() < since {
			continue // "what's changed since T" — skip anything older
		}
		filtered = append(filtered, n)
	}

	// A project scope that matches no notes is usually a NAMING mismatch, not an
	// empty store — agents guess the directory name ("simproj") while the store's
	// vocabulary says something else ("tinytool"). Warn on stderr (tasks may
	// still match below, so this can't be a hard failure) — a silent "0 notes"
	// reads as "those notes don't exist" to an agent.
	if *project != "" && len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "index: no notes carry project %q — the store may use a different name; check the vocabulary with `nt tags`, or search by topic (`nt search`)\n", *project)
	}

	// A scoping folder that matches nothing is almost always a typo — a silent
	// "0 notes" success reads as "those notes don't exist" to an agent.
	if prefix != "" && !rootOnly && len(filtered) == 0 {
		known := make([]string, 0, len(folderSeen))
		for f := range folderSeen {
			if f != "" && f != "." {
				known = append(known, f)
			}
		}
		sort.Strings(known)
		return fail(fmt.Errorf("index: no notes under folder %q — folders here: %s (use `--folder .` for root notes)", prefix, strings.Join(known, ", ")))
	}

	// Tier the DEFAULT view of a large store: pinned (standing knowledge) +
	// recent stubs in full, the long tail as per-folder counts. Any explicit
	// scope (--all/--tag/--folder/--updated-since) means the caller is already
	// narrowing — show every match, exactly as before.
	scoped := *all || prefix != "" || len(tags) > 0 || since != "" || *project != ""
	tiers := note.Tiers{Recent: filtered}
	if !scoped {
		tiers = note.TierIndex(filtered, time.Now())
	}

	toStubs := func(ns []*note.Note, tier string) []indexNote {
		out := make([]indexNote, 0, len(ns))
		for _, n := range ns {
			updated := n.Updated
			if updated == "" {
				updated = n.Created
			}
			desc := n.Description(160)
			if desc == n.Title {
				desc = "" // a description that echoes the title is pure token waste
			}
			out = append(out, indexNote{
				ID: n.ID, Title: n.Title, Description: desc, Source: n.Source,
				Tags: n.Tags, Folder: noteFolder(n), Updated: shortDate(updated), Tier: tier,
			})
		}
		return out
	}
	byFolderTitle := func(stubs []indexNote) {
		sort.SliceStable(stubs, func(i, j int) bool {
			if stubs[i].Folder != stubs[j].Folder {
				return stubs[i].Folder < stubs[j].Folder
			}
			return stubs[i].Title < stubs[j].Title
		})
	}

	// Under tiering a --limit shrinks the recent tier and the overflow joins the
	// rollup counts, so the arithmetic (pinned+recent+older == total) stays exact.
	tiers.LimitRecent(*limit)
	var pinned, stubs []indexNote
	if tiers.Tiered {
		pinned = toStubs(tiers.Pinned, "pinned")
		byFolderTitle(pinned)
		stubs = toStubs(tiers.Recent, "recent") // already newest-first from TierIndex
	} else {
		stubs = toStubs(tiers.Recent, "")
		byFolderTitle(stubs)
	}
	noteTotal := len(filtered)
	if !tiers.Tiered && *limit > 0 && len(stubs) > *limit {
		stubs = stubs[:*limit]
	}

	// Active tasks (open + doing, unblocked, by urgency) plus a few recent
	// completions so a resuming reader sees what's already handled, not only what's
	// open. No bodies.
	var active, blockedTasks, recent []*task.Task
	if !*noTasks {
		if d, err := e.Read(); err == nil {
			blocked := task.BlockedIDs(d.Tasks())
			cur := workstream.Scope(*ws)
			var scoped []*task.Task
			for _, t := range d.Tasks() {
				if cur != "" && !workstream.Visible(t.Key("ws"), cur) {
					continue
				}
				keep := true
				for _, want := range tags {
					if !contains(t.Tags(), want) && !contains(t.Projects(), want) {
						keep = false // a tag scope matches @tag or +project — projects ARE the task-side project identity
						break
					}
				}
				if keep && *project != "" && !contains(t.Projects(), *project) {
					keep = false
				}
				if !keep {
					continue
				}
				scoped = append(scoped, t)
				if !t.Done {
					if blocked[t.ID()] {
						blockedTasks = append(blockedTasks, t)
					} else {
						active = append(active, t)
					}
				}
			}
			task.SortByUrgency(active)
			recent = task.CompletedSince(scoped, "")
			if len(recent) > 5 {
				recent = recent[:5]
			}
		}
	}

	if *asJSON {
		if stubs == nil {
			stubs = []indexNote{} // an empty catalog is [], never null
		}
		shown := stubs
		if tiers.Tiered {
			shown = append(append([]indexNote{}, pinned...), stubs...)
			payload := map[string]any{
				"notes": shown, "tiered": true,
				"olderByFolder": tiers.OlderByFolder, "olderTotal": tiers.OlderTotal,
				"noteTotal": noteTotal,
				"hint":      "tiered catalog: pinned (standing rules/memory/ref) + notes changed in the last 14d; olderByFolder counts the rest — expand with --folder <f> (`.` = root notes), --tag <t>, or --all",
			}
			if !*noTasks {
				payload["tasks"] = tasksToJSON(active, map[*task.Task]int{})
				payload["recentlyDone"] = tasksToJSON(recent, map[*task.Task]int{})
				if len(blockedTasks) > 0 {
					payload["blocked"] = tasksToJSON(blockedTasks, map[*task.Task]int{})
				}
			}
			return printJSON(payload)
		}
		payload := map[string]any{"notes": shown}
		if noteTotal > len(stubs) {
			payload["truncated"] = true
			payload["noteTotal"] = noteTotal
		}
		if !*noTasks {
			payload["tasks"] = tasksToJSON(active, map[*task.Task]int{})
			payload["recentlyDone"] = tasksToJSON(recent, map[*task.Task]int{})
			if len(blockedTasks) > 0 {
				// A resumer reading only the index must still learn blocked work
				// exists — otherwise the plan looks smaller than it is.
				payload["blocked"] = tasksToJSON(blockedTasks, map[*task.Task]int{})
			}
		}
		return printJSON(payload)
	}

	printStubDated := func(s indexNote) {
		line := fmt.Sprintf("- `%s` %s", shortID(s.ID), s.Title)
		if s.Description != "" {
			line += " — " + s.Description
		}
		if len(s.Tags) > 0 {
			line += "  @" + strings.Join(s.Tags, " @")
		}
		if s.Updated != "" {
			line += "  ·upd " + s.Updated
		}
		fmt.Println(line)
	}
	printStub := func(s indexNote, withFolder bool) {
		line := fmt.Sprintf("- `%s` %s", shortID(s.ID), s.Title)
		if s.Description != "" {
			line += " — " + s.Description
		}
		if len(s.Tags) > 0 {
			line += "  @" + strings.Join(s.Tags, " @")
		}
		if s.Source != "" {
			line += "  ·" + s.Source // authorship — who captured it (ownership on a shared store)
		}
		if withFolder && s.Folder != "" && s.Folder != "." {
			line += "  (" + s.Folder + "/)"
		}
		fmt.Println(line)
	}

	switch {
	case tiers.Tiered:
		fmt.Printf("<!-- nt index — %d pinned + %d recent of %d notes, %d active tasks — `nt show <id>` fetches a note; `nt index --all` for the full catalog -->\n",
			len(pinned), len(stubs), noteTotal, len(active))
		if len(pinned) > 0 {
			fmt.Println("\n# Pinned — standing rules · memory · reference")
			lastFolder := "\x00"
			for _, s := range pinned {
				if s.Folder != lastFolder {
					fmt.Printf("\n## %s\n", folderLabel(s.Folder))
					lastFolder = s.Folder
				}
				// Pinned rows carry their date: standing notes never expire out of
				// view, so their age is the only staleness signal a reader gets.
				printStubDated(s)
			}
		}
		if len(stubs) > 0 {
			fmt.Printf("\n# Recent — changed in the last %dd, newest first\n", note.TierRecentDays)
			for _, s := range stubs {
				printStub(s, true)
			}
		}
		if tiers.OlderTotal > 0 {
			fmt.Printf("\n# Older (%d notes — `nt index --folder <f>` to expand, `--all` for everything)\n", tiers.OlderTotal)
			folders := make([]string, 0, len(tiers.OlderByFolder))
			for f := range tiers.OlderByFolder {
				folders = append(folders, f)
			}
			sort.Strings(folders)
			for _, f := range folders {
				label := folderLabel(f)
				if f == "" || f == "." {
					// Only in the rollup: "(root)" alone doesn't say HOW to expand it.
					label = "(root — expand with --folder .)"
				}
				fmt.Printf("- %s — %d note(s)\n", label, tiers.OlderByFolder[f])
			}
		}
	case noteTotal > len(stubs):
		fmt.Printf("<!-- nt index — %d of %d notes (--limit), %d active tasks — narrow with --tag/--folder -->\n", len(stubs), noteTotal, len(active))
	case scoped:
		fmt.Printf("<!-- nt index — %d notes (full listing, untiered), %d active tasks — fetch a note with `nt show <id>` -->\n", len(stubs), len(active))
	default:
		fmt.Printf("<!-- nt index — %d notes, %d active tasks — fetch a note with `nt show <id>` -->\n", len(stubs), len(active))
	}
	if !tiers.Tiered && len(stubs) > 0 {
		fmt.Println("\n# Knowledge base")
		lastFolder := "\x00"
		for _, s := range stubs {
			if s.Folder != lastFolder {
				fmt.Printf("\n## %s\n", folderLabel(s.Folder))
				lastFolder = s.Folder
			}
			printStub(s, false)
		}
	}
	if !*noTasks && len(active) > 0 {
		fmt.Println("\n# Active tasks")
		for _, t := range active {
			mark := " "
			if t.Status() == "doing" {
				mark = "~"
			}
			fmt.Printf("- [%s] %s `%s`\n", mark, strings.TrimSpace(t.Text), shortID(t.ID()))
		}
	}
	if !*noTasks && len(blockedTasks) > 0 {
		fmt.Printf("\n# Blocked (%d — hidden from ready until their blocker completes)\n", len(blockedTasks))
		for _, t := range blockedTasks {
			fmt.Printf("- [⊘] %s `%s`\n", strings.TrimSpace(t.Text), shortID(t.ID()))
		}
	}
	if !*noTasks && len(recent) > 0 {
		fmt.Println("\n# Recently done")
		for _, t := range recent {
			fmt.Printf("- [x] %s `%s`\n", strings.TrimSpace(t.Text), shortID(t.ID()))
		}
	}
	// The empty-state line must be honest about BOTH halves of the catalog —
	// agents reading only this line, not the header comment, mistook a
	// tasks-only "add your first task" nudge for proof the store had no
	// notes at all. --no-tasks means the caller excluded tasks on purpose,
	// so this stays silent on task counts rather than misreport a filter as
	// the store's real state.
	notesEmpty := len(stubs) == 0 && len(pinned) == 0
	tasksEmpty := len(active) == 0 && len(blockedTasks) == 0 && len(recent) == 0
	switch {
	case *noTasks:
		if notesEmpty {
			fmt.Println("no notes — add one: nt note \"title\"")
		}
	case notesEmpty && tasksEmpty:
		msg := "index is empty — 0 notes, 0 tasks"
		if h := freshHint(e); h != "" {
			msg += "\n  add a note:  nt note \"my first note\"" + h
		}
		fmt.Println(msg)
	case notesEmpty:
		fmt.Println("no notes — add one: nt note \"title\"")
	case tasksEmpty:
		fmt.Println("no active tasks — add one: nt add \"title\"")
	}
	return 0
}

func folderLabel(f string) string {
	if f == "" || f == "." {
		return "(root)"
	}
	return f + "/"
}
