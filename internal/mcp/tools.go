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

// toolDefs is the catalog advertised to the agent. Descriptions are written for
// the model — they say when to reach for each tool — and kept terse: prose that
// only restates an obvious property name is dropped (the name carries it), while
// behavioral cues and value formats are kept.
var toolDefs = []toolDef{
	{
		Name:        "nt_ready",
		Description: "Resuming work: open, unblocked tasks by urgency. Returns stable ids for nt_done/nt_update.",
		InputSchema: obj(map[string]any{
			"source":  st(),
			"tag":     st(),
			"project": st(),
		}),
	},
	{
		Name:        "nt_add",
		Description: "Capture a task. discovered_from chains work surfaced while doing another task.",
		InputSchema: obj(map[string]any{
			"text":            st(),
			"priority":        enum("high", "med", "low"),
			"due":             sp("today|tomorrow|fri|+3d|YYYY-MM-DD"),
			"project":         st(),
			"tags":            at(),
			"discovered_from": sp("id of the originating task"),
			"source":          st(),
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
			"id":       st(),
			"status":   enum("open", "doing", "blocked", "done"),
			"priority": enum("high", "med", "low"),
			"due":      sp("today|+3d|YYYY-MM-DD"),
		}, "id"),
	},
	{
		Name:        "nt_note",
		Description: "Save a note (finding/decision/dead-end) — capture the WHY. Set folder to file it under notes/<folder>/; recall returns the body.",
		InputSchema: obj(map[string]any{
			"title":  st(),
			"body":   sp("markdown"),
			"tags":   at(),
			"folder": sp("subfolder to file under, e.g. ref or decisions/auth (created as needed)"),
			"source": st(),
		}, "title"),
	},
	{
		Name:        "nt_recall",
		Description: "Read back earlier tasks and notes (bodies included) to restore prior-session context.",
		InputSchema: obj(map[string]any{
			"source": st(),
			"since":  sp("on/after YYYY-MM-DD"),
		}),
	},
	{
		Name:        "nt_log",
		Description: "Completed tasks, newest first.",
		InputSchema: obj(map[string]any{
			"since":  sp("on/after YYYY-MM-DD"),
			"days":   it(),
			"source": st(),
		}),
	},
	{
		Name:        "nt_search",
		Description: "Find notes and tasks by text and/or tag — the KB's retrieval verb. Pass query, tag, or both.",
		InputSchema: obj(map[string]any{
			"query": sp("text to match in titles + bodies (optional if tag is set)"),
			"tag":   sp("only items with this tag"),
			"type":  enum("note", "task", "all"),
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
}
