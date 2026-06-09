// Package apitypes defines the JSON wire contract for the web SPA's /api/*
// endpoints. It is the single source of truth: the Go handlers (internal/web)
// marshal these structs, and tygo generates the TypeScript types from this file
// (internal/web/frontend/src/lib/api-types.ts) — so the front-end and back-end
// can't drift. It is a pure data package (no imports) so codegen stays trivial.
package apitypes

// State is GET /api/state — capabilities + headline counts for the shell.
type State struct {
	CanEdit   bool     `json:"canEdit"`
	CSRF      string   `json:"csrf"` // echo as X-CSRF on writes; "" when read-only
	Version   string   `json:"version"`
	OpenCount int      `json:"openCount"`
	NoteCount int      `json:"noteCount"`
	Sources   []string `json:"sources"`
	Warning   string   `json:"warning,omitempty"` // non-empty when the store couldn't be fully read
}

// NoteLink is a link to a note (sidebar index, search results, prev/next).
type NoteLink struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// TreeNode is one entry in the sidebar's folder/note tree.
type TreeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"` // folder key; "" for notes
	URL      string     `json:"url"`
	IsNote   bool       `json:"isNote"`
	Children []TreeNode `json:"children,omitempty"`
}

// NotesIndex is GET /api/notes — the tree plus a flat index.
type NotesIndex struct {
	Tree  []TreeNode `json:"tree"`
	Index []NoteLink `json:"index"`
}

// Task is one todo.txt task projected for the UI.
type Task struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Status  string   `json:"status"`
	Due     string   `json:"due,omitempty"`
	Source  string   `json:"source,omitempty"`
	Project string   `json:"project,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Blocker string   `json:"blocker,omitempty"`
}

// TaskGroup is tasks bucketed by status (open/doing/blocked/done).
type TaskGroup struct {
	Status string `json:"status"`
	Tasks  []Task `json:"tasks"`
}

// TasksResponse is GET /api/tasks and the body returned by task writes.
type TasksResponse struct {
	Groups []TaskGroup `json:"groups"`
}

// Backlink is one "Linked from" entry on a note page.
type Backlink struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Text   string `json:"text"`
	IsNote bool   `json:"isNote"`
}

// TaskRef is one task that references a note (the task↔note moat).
type TaskRef struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Source string `json:"source"`
}

// NoteView is GET /api/notes/{handle} — the fully rendered note page.
type NoteView struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Folder    string     `json:"folder"`
	File      string     `json:"file"`
	Crumbs    []string   `json:"crumbs"`
	Source    string     `json:"source"`
	Created   string     `json:"created"`
	Tags      []string   `json:"tags"`
	BodyHTML  string     `json:"bodyHTML"`
	Backlinks []Backlink `json:"backlinks"`
	TaskRefs  []TaskRef  `json:"taskRefs"`
	Prev      *NoteLink  `json:"prev,omitempty"`
	Next      *NoteLink  `json:"next,omitempty"`
	ETag      string     `json:"etag"`
}

// RawNote is GET /api/notes/{handle}/raw — the on-disk text + its validator.
type RawNote struct {
	Text string `json:"text"`
	ETag string `json:"etag"`
}

// ActivityEvent is one item in the activity feed (When is RFC3339).
type ActivityEvent struct {
	When   string `json:"when"`
	Action string `json:"action"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Title  string `json:"title"`
	URL    string `json:"url,omitempty"`
}

// ActivityDay groups events under a calendar date.
type ActivityDay struct {
	Date   string          `json:"date"`
	Events []ActivityEvent `json:"events"`
}

// ActivityResponse is GET /api/activity.
type ActivityResponse struct {
	Days    []ActivityDay `json:"days"`
	Sources []string      `json:"sources"`
}

// SearchResult is one ranked search hit: title matches rank first (no snippet),
// body matches carry the matching line as a snippet for context.
type SearchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Snippet string `json:"snippet,omitempty"` // matching line (body hits only)
}

// SearchResponse is GET /api/search.
type SearchResponse struct {
	Results   []SearchResult `json:"results"`
	Truncated bool           `json:"truncated,omitempty"` // more matched than were returned
}

// Tag is one entry in the tag vocabulary with its usage count.
type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TagsResponse is GET /api/tags.
type TagsResponse struct {
	Tags []Tag `json:"tags"`
}

// OrphansResponse is GET /api/orphans — notes with no links in or out.
type OrphansResponse struct {
	Notes []NoteLink `json:"notes"`
}

// GraphNode is one entity in the knowledge graph (Deg = link degree, for sizing).
type GraphNode struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"` // "note" | "task"
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Folder string   `json:"folder"`
	Source string   `json:"source"`
	Tags   []string `json:"tags"`
	Deg    int      `json:"deg"`
}

// GraphLink is one wikilink edge as a pair of indices into GraphData.Nodes.
type GraphLink struct {
	S int `json:"s"`
	T int `json:"t"`
}

// GraphData is GET /api/graph — the note↔note wikilink graph.
type GraphData struct {
	Nodes     []GraphNode `json:"nodes"`
	Links     []GraphLink `json:"links"`
	Truncated bool        `json:"truncated,omitempty"` // capped to the most-connected nodes (E4)
}

// CreatedNote is the result of POST /api/notes — the new note's stable handle
// and its URL, so the client can navigate straight to it.
type CreatedNote struct {
	Handle string `json:"handle"`
	URL    string `json:"url"`
}

// JournalDay is one existing daily note (date + the note's stable handle).
type JournalDay struct {
	Date   string `json:"date"`   // YYYY-MM-DD
	Handle string `json:"handle"` // note handle, for the note view
}

// JournalResponse is GET /api/journal — the daily-notes index for the journal UI.
type JournalResponse struct {
	Today  string       `json:"today"`  // today's date in the server's local time
	Folder string       `json:"folder"` // subfolder daily notes live under (e.g. "journal")
	Days   []JournalDay `json:"days"`   // existing daily notes, newest first
}
