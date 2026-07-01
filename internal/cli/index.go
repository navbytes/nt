package cli

import (
	"flag"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
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
	Updated     string   `json:"updated,omitempty"`
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
	limit := fs.Int("limit", 0, "cap the note catalog to N (0 = all); scope with --tag/--folder for large stores")
	var tags stringSlice
	fs.Var(&tags, "tag", "only notes with this tag (repeatable, AND)")
	flags, _ := splitArgs(args, map[string]bool{"json": true, "no-tasks": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	e, ok := engine()
	if !ok {
		return 1
	}

	notes := note.Active(mustNotes(e))
	prefix := strings.Trim(*folder, "/")
	var stubs []indexNote
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
		updated := n.Updated
		if updated == "" {
			updated = n.Created
		}
		stubs = append(stubs, indexNote{
			ID: n.ID, Title: n.Title, Description: n.Description(160),
			Tags: n.Tags, Folder: noteFolder(n), Updated: shortDate(updated),
		})
	}
	sort.SliceStable(stubs, func(i, j int) bool {
		if stubs[i].Folder != stubs[j].Folder {
			return stubs[i].Folder < stubs[j].Folder
		}
		return stubs[i].Title < stubs[j].Title
	})
	noteTotal := len(stubs)
	if *limit > 0 && len(stubs) > *limit {
		stubs = stubs[:*limit]
	}

	// Active tasks (open + doing, unblocked, by urgency) plus a few recent
	// completions so a resuming reader sees what's already handled, not only what's
	// open. No bodies.
	var active, recent []*task.Task
	if !*noTasks {
		if d, err := e.Read(); err == nil {
			blocked := task.BlockedIDs(d.Tasks())
			var scoped []*task.Task
			for _, t := range d.Tasks() {
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
				if !t.Done && !blocked[t.ID()] {
					active = append(active, t)
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
		payload := map[string]any{"notes": stubs}
		if noteTotal > len(stubs) {
			payload["truncated"] = true
			payload["noteTotal"] = noteTotal
		}
		if !*noTasks {
			payload["tasks"] = tasksToJSON(active, map[*task.Task]int{})
			payload["recentlyDone"] = tasksToJSON(recent, map[*task.Task]int{})
		}
		return printJSON(payload)
	}

	if noteTotal > len(stubs) {
		fmt.Printf("<!-- nt index — %d of %d notes (--limit), %d active tasks — narrow with --tag/--folder -->\n", len(stubs), noteTotal, len(active))
	} else {
		fmt.Printf("<!-- nt index — %d notes, %d active tasks — fetch a note with `nt show <id>` -->\n", len(stubs), len(active))
	}
	if len(stubs) > 0 {
		fmt.Println("\n# Knowledge base")
		lastFolder := "\x00"
		for _, s := range stubs {
			if s.Folder != lastFolder {
				fmt.Printf("\n## %s\n", folderLabel(s.Folder))
				lastFolder = s.Folder
			}
			line := fmt.Sprintf("- `%s` %s", shortID(s.ID), s.Title)
			if s.Description != "" {
				line += " — " + s.Description
			}
			if len(s.Tags) > 0 {
				line += "  @" + strings.Join(s.Tags, " @")
			}
			fmt.Println(line)
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
