package mcp

// toolDef is one entry in the MCP tools/list response.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// Schema helpers. Bare forms (st/at/it) omit per-property descriptions where the
// property name is self-documenting — every dropped description removes the whole
// "description" key from the advertised schema, shrinking the per-session token
// cost. sp/enum carry meaning the model would otherwise get wrong (formats,
// closed value sets), so those stay.
func st() map[string]any            { return map[string]any{"type": "string"} }
func sp(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func enum(vals ...string) map[string]any {
	return map[string]any{"type": "string", "enum": vals}
}
func at() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

// wsArg is the shared `workstream` property. It isolates parallel agents sharing
// one store: tasks scope to a workstream, notes stay shared. Usually omitted —
// the identity comes from NT_WORKSTREAM (or "auto"). Pass "*" on a read to see
// every workstream's tasks.
var wsArg = sp(`workstream id; omit to use NT_WORKSTREAM. "*" reads all workstreams' tasks`)

// toolDefs is the catalog advertised to the agent. Descriptions are written for
// the model — they say when to reach for each tool — and kept terse: prose that
// only restates an obvious property name is dropped (the name carries it), while
// behavioral cues and value formats are kept.
var toolDefs = []toolDef{
	{
		Name:        "nt_status",
		Description: "Resuming work on a project/area: in-progress + blocked first, then open unblocked tasks by urgency, recent completions, and linked notes — one call for the full picture. Scope with project and/or tag (omit both for everything). Returns stable task ids for nt_update.",
		InputSchema: obj(map[string]any{
			"project":    st(),
			"tag":        st(),
			"workstream": wsArg,
		}),
	},
	{
		Name:        "nt_view",
		Description: "Run one of the user's saved smart views (their named task queries). Omit name to list them first.",
		InputSchema: obj(map[string]any{
			"name": sp("saved view name; omit to list available views"),
		}),
	},
	{
		Name:        "nt_add",
		Description: "Capture a task. text = a short, verb-first title (~60 chars, for skimming). Put any detail/reasoning/steps in body — it's saved as the task's linked note so the title stays clean. discovered_from chains work surfaced while doing another task.",
		InputSchema: obj(map[string]any{
			"text":            sp("short actionable title, verb-first, ~60 chars"),
			"body":            sp("detail/reasoning/steps — saved as a linked note (markdown)"),
			"priority":        enum("high", "med", "medium", "low"),
			"due":             sp("today|tomorrow|fri|+3d|YYYY-MM-DD"),
			"project":         st(),
			"tags":            at(),
			"discovered_from": sp("id of the originating task"),
			"blocked_by":      sp("id of a task that must complete first (the reverse of blocks)"),
			"blocks":          sp("id of a task THIS new task blocks"),
			"source":          st(),
			"workstream":      sp(`workstream to stamp on the task; omit to use NT_WORKSTREAM; "*" or "" stores it unscoped (shared backlog)`),
		}, "text"),
	},
	{
		Name:        "nt_update",
		Description: "Change a task's status, priority, or due by id. status:\"done\" completes it (and spawns the next occurrence if recurring).",
		InputSchema: obj(map[string]any{
			"id":         st(),
			"status":     enum("open", "doing", "blocked", "done"),
			"priority":   enum("high", "med", "medium", "low"),
			"due":        sp("today|tomorrow|fri|+3d|YYYY-MM-DD"),
			"blocked_by": sp("id of a task that must complete first (the reverse of blocks)"),
			"blocks":     sp(`id of a task this task blocks; "none" clears the edge`),
			"workstream": sp(`reassign to a workstream; "*" releases to the shared backlog`),
		}, "id"),
	},
	{
		Name:        "nt_note",
		Description: "Save a note (finding/decision/dead-end) — capture the WHY. Set description to a one-line summary; it's what nt_index shows. The note is always created; if near-duplicates exist the response includes a `similar` list — check it, and consolidate with nt_archive superseded_by=<id> if you truly doubled one. Use supersede=<id> to replace an existing note (the old one retires from views). Unlike the CLI (which refuses near-duplicates), this tool never refuses — consolidate afterwards via the similar list.",
		InputSchema: obj(map[string]any{
			"title":       st(),
			"body":        sp("markdown"),
			"description": sp("one-line summary shown in nt_index (progressive disclosure)"),
			"tags":        at(),
			"kind":        map[string]any{"type": "string", "enum": []string{"lesson", "decision", "ref", "rule", "memory"}, "description": "note class lesson|decision|ref|rule|memory — tags it and files it in the canonical folder (lessons/, decisions/, ref/, rules/, memory/); memory files under memory/ with tag memory-core — the always-loaded core-memory layer; prefer this over inventing a folder"},
			"folder":      sp("subfolder, e.g. ref or decisions/auth (kind picks a canonical one; explicit folder wins)"),
			"project":     sp("project this note belongs to — stored as project: frontmatter so nt_recall's project boost finds it"),
			"source":      st(),
			"if_exists":   map[string]any{"type": "string", "enum": []string{"create", "return", "error"}, "description": `what to do when a note with this exact title/slug already exists (folder-scoped if folder is set): "create" (default) creates anyway; "return" writes NOTHING and returns the existing note's id + mtime token so you edit it via nt_note_edit instead; "error" refuses. Prefer "return" for topic notes so one topic stays one note`},
			"supersede":   sp("id of an existing note this replaces; the old note retires from active views"),
			"force":       map[string]any{"type": "boolean", "description": "create even if a near-duplicate exists"},
			"valid_from":  sp("optional: this fact is only true from this date/time on (YYYY-MM-DD or RFC3339)"),
			"valid_until": sp("optional: this fact stops being true after this date/time (YYYY-MM-DD or RFC3339) — nt_recall down-ranks and flags it expired past this, never hides it"),
		}, "title"),
	},
	{
		Name:        "nt_note_edit",
		Description: "Fix an EXISTING note in place — no new id, no retired-note churn (unlike nt_note supersede:, which replaces it with a brand new note). Exactly one of append / body / old_string+new_string per call: append adds to the end; body replaces the whole thing (build the full text yourself and send it — there's no partial form of body); old_string+new_string patches ONE exact match (must appear exactly once in the current body, or the call is refused — include more surrounding text to disambiguate). description replaces the one-line summary and can be combined with any of the three.",
		InputSchema: obj(map[string]any{
			"handle":       sp("note id, slug, or title"),
			"id":           sp("alias for handle — pass a stub's id directly"),
			"append":       sp("markdown to add to the end of the body"),
			"body":         sp("replace the whole body with this literal text"),
			"old_string":   sp("exact existing text in the body to replace — must match exactly once"),
			"new_string":   sp("replacement for old_string (empty deletes the matched text); required together with old_string"),
			"description":  sp("replace the note's one-line description (frontmatter)"),
			"expect_mtime": sp("optional: the `mtime` token from a prior nt_get of this note — refuses instead of overwriting if the note changed on disk since (best-effort; omit if you don't have one)"),
			"valid_from":   sp("set the valid_from date/time (YYYY-MM-DD or RFC3339)"),
			"valid_until":  sp("set the valid_until date/time (YYYY-MM-DD or RFC3339)"),
		}),
	},
	{
		Name:        "nt_relink",
		Description: "Fix a wrong outbound [[link]] in a note's body: rewrite [[old]] → [[new]] (nt_mv only fixes inbound links on rename). Use it to repair a dangling reference nt_note flagged.",
		InputSchema: obj(map[string]any{
			"handle": sp("the note whose body to edit (id/slug/title)"),
			"old":    sp("the current [[target]] text to replace"),
			"new":    sp("the correct [[target]] (must resolve to a note)"),
		}, "handle", "old", "new"),
	},
	{
		Name:        "nt_index",
		Description: "Resuming work: the KB catalog. One stub per note (id, title, one-line description, tags, folder) — NO bodies — plus active (open+doing) tasks and a few recent completions (recentlyDone); blocked tasks are listed separately. Load this first, then nt_get the few notes you need or nt_search by topic. Cheap and bounded: large stores return a TIERED catalog (tiered=true) — pinned standing notes + stubs changed in the last 14d, with the older remainder as olderByFolder counts; expand a folder with the folder filter, or pass all:true for every stub. Scope with tag/folder.",
		InputSchema: obj(map[string]any{
			"tag":           sp("only notes/tasks with this tag"),
			"folder":        sp(`only notes under this folder, e.g. ref ("." = root notes)`),
			"all":           map[string]any{"type": "boolean", "description": "full catalog: every note stub, no tiering (large stores tier by default)"},
			"limit":         map[string]any{"type": "integer", "description": "cap the note catalog to N (truncated=true when more exist); scope with tag/folder for big stores"},
			"updated_since": sp("only notes changed on/after this date (14d = last 14 days | today | YYYY-MM-DD) — 'what changed since last session'"),
			"format": map[string]any{
				"type": "string", "enum": []string{"json", "compact"},
				"description": "'compact' for terse one-line-per-item text (cheaper — prefer for the session-start load); default is JSON",
			},
			"workstream": wsArg,
		}),
	},
	{
		Name:        "nt_get",
		Description: "Fetch one note's full body by handle (id, slug, or title) — the on-demand half of the index. With section, returns only the markdown block under that heading.",
		InputSchema: obj(map[string]any{
			"handle":  sp("note id, slug, or title (from nt_index / nt_search)"),
			"id":      sp("alias for handle — pass a stub's id directly"),
			"section": sp("optional: a heading within the note to return just that block"),
		}),
	},
	{
		Name:        "nt_search",
		Description: "Find notes and tasks by EXACT text and/or tag — reach for it when you know the words that appear in the note (use nt_recall for paraphrased/conceptual matching, nt_index for the whole catalog). Store-wide: results are never workstream-scoped, so it finds every agent's tasks. Returns ranked STUBS (id, title, description, snippet) not bodies; nt_get the id you want. Title matches rank first; truncated=true when more exist. At least one of query/tag is required; full=true to inline bodies.",
		InputSchema: obj(map[string]any{
			"query":            sp("text to match in titles + bodies (optional if tag is set)"),
			"tag":              sp("only items with this tag"),
			"type":             enum("note", "task", "all"),
			"limit":            map[string]any{"type": "integer", "description": "max results (default 8)"},
			"full":             map[string]any{"type": "boolean", "description": "return full note bodies instead of stubs"},
			"include_archived": map[string]any{"type": "boolean", "description": "also search retired notes (archived/superseded) — the deep sweep nt_recall's escalate hint suggests; retired hits are flagged archived/supersededBy"},
		}),
	},
	{
		Name:        "nt_recall",
		Description: "Learn from past sessions: given a free-text description of what you're ABOUT to do, return the most relevant notes — lessons/gotchas first — so you don't repeat a recorded mistake. Unlike nt_search (exact substring), recall stems and expands synonyms, so a paraphrase still finds the note. Call this at the start of a task (e.g. context:'adding a cache layer to the API'); omit context with lessons_only:true to list every recorded lesson (newest first). Cheap: returns compact stubs, no bodies. Results with lesson:true are recorded mistakes — read them (nt_get) before proceeding.",
		InputSchema: obj(map[string]any{
			"context":      sp("what you're about to work on, in plain words — the more specific, the better the recall (optional with lessons_only)"),
			"project":      sp(`prefer this project's notes in ranking (soft boost, never a filter) — matches note tags, folder segments, and project: frontmatter; omit to use the workstream identity, "none" disables`),
			"limit":        map[string]any{"type": "integer", "description": "max results (default 8)"},
			"lessons_only": map[string]any{"type": "boolean", "description": "restrict to notes tagged `lesson` (recorded mistakes only); with no context, lists every lesson newest-first"},
		}),
	},
	{
		Name:        "nt_links",
		Description: "Forward links and backlinks for a note or task — follow the knowledge graph.",
		InputSchema: obj(map[string]any{"handle": sp("a note handle (slug/title/id) or task id")}, "handle"),
	},
	{
		Name:        "nt_mindmap",
		Description: "Render a mind map of a note as a Mermaid `mindmap` diagram — a compact visual summary you can drop into a note (nt's web viewer, Obsidian, and GitHub render it) via nt_note/nt_note_edit. source 'outline' (default) maps the note's own headings + nested lists; source 'links' maps the notes it connects to via [[wikilinks]]. Use depth to cap levels, no_fence for the raw diagram, or format 'json' for the raw tree. Errors if there's nothing to map.",
		InputSchema: obj(map[string]any{
			"handle":   sp("a note handle (slug/title/id)"),
			"source":   enum("outline", "links"),
			"depth":    map[string]any{"type": "integer", "description": "limit to N levels below the root (omit/0 = all; default 3 for links)"},
			"no_fence": map[string]any{"type": "boolean", "description": "return the raw mindmap without the ```mermaid``` code fence"},
			"format":   enum("mermaid", "json"),
		}, "handle"),
	},
	{
		Name:        "nt_mv",
		Description: "Refile a note: rename or move it into a folder, rewriting every [[link]] to it.",
		InputSchema: obj(map[string]any{
			"handle": sp("the note to move (slug/title/id)"),
			"dest":   sp("new name or folder/path under notes/, e.g. ref/auth"),
		}, "handle", "dest"),
	},
	{
		Name:        "nt_tag",
		Description: "Add/remove tags on a note during curation (preserves other frontmatter). At least one of add/remove is required.",
		InputSchema: obj(map[string]any{
			"handle": sp("the note (slug/title/id)"),
			"add":    at(),
			"remove": at(),
		}, "handle"),
	},
	{
		Name:        "nt_rm",
		Description: "Remove a mistaken/duplicate TASK permanently by stable id — journaled, so `nt undo` restores it. Finished work is nt_update status:\"done\", not rm; a stale NOTE is nt_archive, not rm.",
		InputSchema: obj(map[string]any{
			"id": sp("stable task id (from nt_status, nt_index, or nt_search) — positional task:N is refused"),
		}, "id"),
	},
	{
		Name:        "nt_doctor",
		Description: "Store health check — read-only, never writes (unlike `nt doctor`, which also fixes task-file issues). Reports dangling [[links]], task-file problems (duplicate/missing ids, dependency warnings) that need `nt doctor` (the CLI) to fix, and notes past their valid_until. Call when something feels off, or before a big cleanup pass.",
		InputSchema: obj(map[string]any{}),
	},
	{
		Name:        "nt_distill",
		Description: "List every near-duplicate note pair in the store (by title) — read-only, proposes nothing on its own. For each pair, review both (nt_get) and either fold one into the other (nt_note_edit append/old_string+new_string) then retire the old one (nt_archive with superseded_by set to the kept id), or tag one 'distinct' (nt_tag) if it's a deliberate fork. Always confirm with the user before merging — never merge silently.",
		InputSchema: obj(map[string]any{
			"limit": map[string]any{"type": "integer", "description": "cap the number of pairs returned (default: all)"},
		}),
	},
	{
		Name:        "nt_archive",
		Description: "Retire a stale note from index/search/recall/status — reversible, the file stays on disk. Set superseded_by when another note replaces it (reconciling duplicates): the old note retires with a pointer preserving the trail. Set undo to bring an archived note back.",
		InputSchema: obj(map[string]any{
			"handle":        sp("the note to retire (slug/title/id)"),
			"superseded_by": sp("optional: the note that replaces this one (id/slug/title) — records a superseded_by pointer instead of a plain archive"),
			"undo":          map[string]any{"type": "boolean", "description": "unarchive instead"},
		}, "handle"),
	},
}
