package cli

import (
	"flag"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

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

// noteChangedDate is the note's effective change date (YYYY-MM-DD): the later of
// its file mtime (catches external edits) and its frontmatter updated/created.
func noteChangedDate(n *note.Note, frontmatter string) string {
	d := shortDate(frontmatter)
	if !n.ModTime.IsZero() {
		if m := n.ModTime.Format("2006-01-02"); m > d {
			d = m
		}
	}
	return d
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
func cmdIndex(args []string) int {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	folder := fs.String("folder", "", "only notes under this folder")
	asJSON := fs.Bool("json", false, "machine-readable output")
	noTasks := fs.Bool("no-tasks", false, "omit the active-task section")
	all := fs.Bool("all", false, "full catalog: every note stub, no tiering (large stores tier by default)")
	limit := fs.Int("limit", 0, "cap the note catalog to N (0 = all); scope with --tag/--folder for large stores")
	updatedSince := fs.String("updated-since", "", "only notes changed on/after this date (today|fri|+3d|YYYY-MM-DD) — 'what's new since last session'")
	sinceAlias := fs.String("since", "", "alias for --updated-since (matches `nt log --since`)")
	ws := fs.String("workstream", "", `scope tasks to a workstream (default: NT_WORKSTREAM; "*" = all)`)
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
		d, ok := parseDate(s)
		if !ok {
			return usageErr(fmt.Errorf("index: --updated-since: unrecognized date %q (try today|fri|+3d|YYYY-MM-DD)", s))
		}
		since = d
	}
	e, ok := engine()
	if !ok {
		return 1
	}

	notes := note.Active(mustNotes(e))
	prefix := strings.Trim(*folder, "/")
	var filtered []*note.Note
	for _, n := range notes {
		if n.Reserved() {
			continue // task-detail notes aren't part of the KB catalog
		}
		if prefix != "" && !strings.HasPrefix(n.Rel, prefix+"/") {
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
		if since != "" && n.ChangedDate() < since {
			continue // "what's changed since T" — skip anything older
		}
		filtered = append(filtered, n)
	}

	// Tier the DEFAULT view of a large store: pinned (standing knowledge) +
	// recent stubs in full, the long tail as per-folder counts. Any explicit
	// scope (--all/--tag/--folder/--updated-since) means the caller is already
	// narrowing — show every match, exactly as before.
	scoped := *all || prefix != "" || len(tags) > 0 || since != ""
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
			out = append(out, indexNote{
				ID: n.ID, Title: n.Title, Description: n.Description(160), Source: n.Source,
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
	if *limit > 0 && len(stubs) > *limit {
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
					if !contains(t.Tags(), want) {
						keep = false
						break
					}
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
				"hint":      "tiered catalog: pinned (standing rules/memory/ref) + notes changed in the last 14d; olderByFolder counts the rest — expand with --folder <f>, --tag <t>, or --all",
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
				printStub(s, false)
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
				fmt.Printf("- %s — %d note(s)\n", folderLabel(f), tiers.OlderByFolder[f])
			}
		}
	case noteTotal > len(stubs):
		fmt.Printf("<!-- nt index — %d of %d notes (--limit), %d active tasks — narrow with --tag/--folder -->\n", len(stubs), noteTotal, len(active))
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
	if len(stubs) == 0 && len(active) == 0 && len(recent) == 0 {
		fmt.Println("index is empty" + freshHint(e))
	}
	return 0
}

func folderLabel(f string) string {
	if f == "" || f == "." {
		return "(root)"
	}
	return f + "/"
}
