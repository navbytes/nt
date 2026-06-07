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

func sp(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func ap(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func ip(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

// toolDefs is the catalog advertised to the agent. Descriptions are written for
// the model: they say when to reach for each tool, not just what it does.
var toolDefs = []toolDef{
	{
		Name:        "nt_ready",
		Description: "Start here when resuming work: list open, UNBLOCKED tasks (no done, no dependency-blocked) ordered by urgency. Returns stable task ids to use with nt_done/nt_update.",
		InputSchema: obj(map[string]any{
			"source":  sp("only tasks from this origin, e.g. \"claude\""),
			"tag":     sp("only tasks with this @tag"),
			"project": sp("only tasks in this +project"),
		}),
	},
	{
		Name:        "nt_add",
		Description: "Capture a task so it survives the session. Use --discovered_from to chain work you surfaced while doing another task.",
		InputSchema: obj(map[string]any{
			"text":            sp("the task description (may include inline @tag/+project)"),
			"priority":        sp("high | med | low"),
			"due":             sp("today | tomorrow | fri | +3d | YYYY-MM-DD"),
			"project":         sp("project name (without the +)"),
			"tags":            ap("tag names (without the @)"),
			"discovered_from": sp("id of the task this was surfaced while working on"),
			"source":          sp("origin; defaults to \"claude\""),
		}, "text"),
	},
	{
		Name:        "nt_done",
		Description: "Mark a task complete by id (spawns the next occurrence if it recurs).",
		InputSchema: obj(map[string]any{"id": sp("the task id from nt_ready/nt_recall")}, "id"),
	},
	{
		Name:        "nt_update",
		Description: "Change a task's status, priority, or due date by id.",
		InputSchema: obj(map[string]any{
			"id":       sp("the task id"),
			"status":   sp("open | doing | blocked | done"),
			"priority": sp("high | med | low"),
			"due":      sp("today | tomorrow | +3d | YYYY-MM-DD"),
		}, "id"),
	},
	{
		Name:        "nt_note",
		Description: "Save a note — a finding, decision, constraint, or dead-end. Capture the WHY here, not just todos; recall returns the full body next session.",
		InputSchema: obj(map[string]any{
			"title":  sp("short title"),
			"body":   sp("the note content (markdown)"),
			"tags":   ap("tag names (without the @)"),
			"source": sp("origin; defaults to \"claude\""),
		}, "title"),
	},
	{
		Name:        "nt_recall",
		Description: "Read back tasks and notes captured earlier (note bodies included) to restore context from prior sessions.",
		InputSchema: obj(map[string]any{
			"source": sp("only items from this origin, e.g. \"claude\""),
			"since":  sp("only items created on/after YYYY-MM-DD"),
		}),
	},
	{
		Name:        "nt_log",
		Description: "List completed tasks, newest first — what was recently finished.",
		InputSchema: obj(map[string]any{
			"since":  sp("completions on/after YYYY-MM-DD"),
			"days":   ip("completions in the last N days"),
			"source": sp("only completions from this origin"),
		}),
	},
}
