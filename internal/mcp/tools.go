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
func it() map[string]any { return map[string]any{"type": "integer"} }

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
		Name:        "nt_ready",
		Description: "Resuming work: open, unblocked tasks by urgency. Returns stable ids for nt_done/nt_update.",
		InputSchema: obj(map[string]any{
			"source":     st(),
			"tag":        st(),
			"project":    st(),
			"workstream": wsArg,
		}),
	},
	{
		Name:        "nt_status",
		Description: "Resuming a project/area: in-progress + blocked first, then open by urgency, recent completions, and linked notes. Scope with project and/or tag (omit both for everything).",
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
			"priority":        enum("high", "med", "low"),
			"due":             sp("today|tomorrow|fri|+3d|YYYY-MM-DD"),
			"project":         st(),
			"tags":            at(),
			"discovered_from": sp("id of the originating task"),
			"source":          st(),
			"workstream":      wsArg,
		}, "text"),
	},
	{
		Name:        "nt_done",
		Description: "Complete a task by id (spawns next occurrence if recurring).",
		InputSchema: obj(map[string]any{"id": st()}, "id"),
	},
	{
		Name:        "nt_update",
		Description: "Change a task's status, priority, or due by id.",
		InputSchema: obj(map[string]any{
			"id":         st(),
			"status":     enum("open", "doing", "blocked", "done"),
			"priority":   enum("high", "med", "low"),
			"due":        sp("today|+3d|YYYY-MM-DD"),
			"workstream": sp(`reassign to a workstream; "*" releases to the shared backlog`),
		}, "id"),
	},
	{
		Name:        "nt_note",
		Description: "Save a note (finding/decision/dead-end) — capture the WHY. Set description to a one-line summary; it's what nt_index shows. Guarded against near-duplicates: if a similar note exists it errors — update that one, supersede it, or set force=true. Use supersede=<id> to replace an existing note (the old one retires from views).",
		InputSchema: obj(map[string]any{
			"title":       st(),
			"body":        sp("markdown"),
			"description": sp("one-line summary shown in nt_index (progressive disclosure)"),
			"tags":        at(),
			"folder":      sp("subfolder, e.g. ref or decisions/auth"),
			"source":      st(),
			"supersede":   sp("id of an existing note this replaces; the old note retires from active views"),
			"force":       map[string]any{"type": "boolean", "description": "create even if a near-duplicate exists"},
		}, "title"),
	},
	{
		Name:        "nt_supersede",
		Description: "Reconcile duplicate/obsolete notes: mark one note as replaced by another. The old note retires from nt_index/nt_search (so a resume sees only the current decision) while a superseded_by pointer preserves the trail.",
		InputSchema: obj(map[string]any{
			"handle": sp("the note being replaced (id/slug/title)"),
			"by":     sp("the note that replaces it (id/slug/title)"),
		}, "handle", "by"),
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
		Description: "Resuming work: the KB catalog. One stub per note (id, title, one-line description, tags, folder) — NO bodies — plus active (open+doing) tasks and a few recent completions (recentlyDone). Load this first, then nt_get the few notes you need or nt_search by topic. Cheap and bounded; replaces dumping every note. Scope with tag/folder.",
		InputSchema: obj(map[string]any{
			"tag":           sp("only notes/tasks with this tag"),
			"folder":        sp("only notes under this folder, e.g. ref"),
			"limit":         map[string]any{"type": "integer", "description": "cap the note catalog to N (truncated=true when more exist); scope with tag/folder for big stores"},
			"updated_since": sp("only notes changed on/after this date (today|+3d|YYYY-MM-DD) — 'what changed since last session'"),
			"format":        sp("'compact' for terse one-line-per-item text (cheaper — prefer for the session-start load); default is JSON"),
			"workstream":    wsArg,
		}),
	},
	{
		Name:        "nt_get",
		Description: "Fetch one note's full body by handle (id, slug, or title) — the on-demand half of the index. With section, returns only the markdown block under that heading.",
		InputSchema: obj(map[string]any{
			"handle":  sp("note id, slug, or title (from nt_index / nt_search)"),
			"section": sp("optional: a heading within the note to return just that block"),
		}, "handle"),
	},
	{
		Name:        "nt_log",
		Description: "Completed tasks, newest first.",
		InputSchema: obj(map[string]any{
			"since":      sp("on/after YYYY-MM-DD"),
			"days":       it(),
			"source":     st(),
			"workstream": wsArg,
		}),
	},
	{
		Name:        "nt_search",
		Description: "Find notes and tasks by text and/or tag — the KB's retrieval verb. Returns ranked STUBS (id, title, description, snippet) not bodies; nt_get the id you want. Title matches rank first; results are capped by limit (default 8) with truncated=true when more exist. Pass query, tag, or both; full=true to inline bodies.",
		InputSchema: obj(map[string]any{
			"query": sp("text to match in titles + bodies (optional if tag is set)"),
			"tag":   sp("only items with this tag"),
			"type":  enum("note", "task", "all"),
			"limit": it(),
			"full":  map[string]any{"type": "boolean", "description": "return full note bodies instead of stubs"},
		}),
	},
	{
		Name:        "nt_links",
		Description: "Forward links and backlinks for a note or task — follow the knowledge graph.",
		InputSchema: obj(map[string]any{"handle": sp("a note handle (slug/title/id) or task id")}, "handle"),
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
		Description: "Add/remove tags on a note during curation (preserves other frontmatter).",
		InputSchema: obj(map[string]any{
			"handle": sp("the note (slug/title/id)"),
			"add":    at(),
			"remove": at(),
		}, "handle"),
	},
	{
		Name:        "nt_archive",
		Description: "Retire a stale/superseded note from recall/search/status — reversible, file stays on disk. Set undo to bring it back.",
		InputSchema: obj(map[string]any{
			"handle": sp("the note to archive (slug/title/id)"),
			"undo":   map[string]any{"type": "boolean", "description": "unarchive instead"},
		}, "handle"),
	},
}
