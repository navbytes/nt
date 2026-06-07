package web

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// md is the shared Markdown engine. goldmark is already in the module graph
// (Glamour pulls it in), so this adds no new dependency. GFM gives tables /
// task lists / strikethrough / autolinks; auto heading IDs let `#heading`
// anchors work. We keep the default (safe) HTML renderer — raw HTML in note
// bodies is escaped, so a note can't inject script into the viewer.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// renderBody converts a note's Markdown to HTML. It first rewrites [[wikilinks]]
// into real links — resolved targets point at /n/<id> (a stable handle, so the
// link survives renames/moves), unresolved or ambiguous ones become dim
// /n/<key>?missing=1 links the viewer styles as broken. ```mermaid``` and other
// fenced blocks are left untouched (mermaid runs client-side; code stays code).
//
// This function is the single render path: a future live-preview endpoint reuses
// it verbatim (seam #3).
func renderBody(body string, doc *task.Doc, notes []*note.Note) (template.HTML, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(rewriteWikilinks(body, doc, notes)), &buf); err != nil {
		return "", err
	}
	out := buf.String()
	// ```mermaid``` → <div class="mermaid"> so mermaid.run() finds it (the
	// escaped source decodes via textContent client-side).
	out = mermaidBlockRe.ReplaceAllString(out, `<div class="mermaid">$1</div>`)
	// Tag resolved wikilinks (query-less /n/ hrefs) so CSS can style them;
	// ?missing=1 links are skipped here and styled via the attribute selector.
	out = wikiClassRe.ReplaceAllString(out, `<a class="wikilink" href="$1">`)
	return template.HTML(out), nil //nolint:gosec // goldmark output is escaped (safe renderer)
}

var (
	mermaidBlockRe = regexp.MustCompile(`(?s)<pre><code class="language-mermaid">(.*?)</code></pre>`)
	wikiClassRe    = regexp.MustCompile(`<a href="(/n/[^"?]+)">`)
)

// rewriteWikilinks replaces [[target]] with Markdown links, but never inside a
// fenced code block (so mermaid sources and code samples are preserved).
func rewriteWikilinks(body string, doc *task.Doc, notes []*note.Note) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, ln := range lines {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = wikilinkRe.ReplaceAllStringFunc(ln, func(m string) string {
			return mdLink(m[2:len(m)-2], doc, notes)
		})
	}
	return strings.Join(lines, "\n")
}

// mdLink turns one wikilink inner string into a Markdown link.
func mdLink(inner string, doc *task.Doc, notes []*note.Note) string {
	key, alias := links.NormalizeTarget(inner)
	it, ok := links.Resolve(inner, doc, notes)
	label := alias
	if label == "" {
		if ok && it.Title != "" {
			label = it.Title
		} else {
			label = key
		}
	}
	label = mdEscapeLabel(label)
	if ok && it.Kind == "note" {
		return "[" + label + "](/n/" + url.PathEscape(it.ID) + ")"
	}
	// unresolved, ambiguous, or a task (no web page in v1) → dim "missing" link
	// that lands on a lookup/disambiguation page.
	return "[" + label + "](/n/" + url.PathEscape(key) + "?missing=1)"
}

var mdLabelEscaper = strings.NewReplacer("[", `\[`, "]", `\]`)

func mdEscapeLabel(s string) string { return mdLabelEscaper.Replace(s) }

// Backlink is one "Linked from" entry for the note page.
type Backlink struct {
	Title  string // note title, or "" for a task source
	URL    string // /n/<id> for a note source; "" for a task
	Text   string // the matching line (shown for task sources)
	IsNote bool
}

// backlinksFor collects de-duplicated backlinks to a note, mapping each source
// file to a note (linkable) or leaving it as a task line.
func backlinksFor(s *store.Store, n *note.Note, notes []*note.Note) []Backlink {
	byPath := make(map[string]*note.Note, len(notes))
	for _, x := range notes {
		byPath[x.Path] = x
	}
	seen := map[string]bool{}
	var out []Backlink
	for _, h := range links.Backlinks(s, n.ID, n.Rel) {
		// Note → note only; task references get their own "Referenced by tasks"
		// panel (tasksReferencing), so they're excluded here to avoid showing
		// the same task in two places.
		src, isNote := byPath[h.Path]
		if !isNote || src.ID == n.ID || seen[src.ID] {
			continue
		}
		seen[src.ID] = true
		// Keep the matching line as a snippet (Notion-style context).
		out = append(out, Backlink{
			Title:  src.Title,
			URL:    "/n/" + url.PathEscape(src.ID),
			Text:   snippet(h.Text),
			IsNote: true,
		})
	}
	return out
}

// snippet trims a matched line to a compact one-liner for context display.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	const max = 120
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// TaskRef is one task that links to the note (the task↔note moat).
type TaskRef struct {
	Text   string
	Status string
	Source string
}

// tasksReferencing returns tasks whose [[links]] resolve to this note — the
// "Referenced by tasks" panel. Reuses links.Resolve, the same resolution the
// CLI/TUI use, over the task Doc already loaded for the request.
func tasksReferencing(doc *task.Doc, n *note.Note, notes []*note.Note) []TaskRef {
	if doc == nil {
		return nil
	}
	var out []TaskRef
	for _, t := range doc.Tasks() {
		for _, raw := range t.Links() {
			if it, ok := links.Resolve(raw, doc, notes); ok && it.Kind == "note" && it.ID == n.ID {
				out = append(out, TaskRef{Text: cleanTaskText(t.Text), Status: t.Status(), Source: t.Source()})
				break
			}
		}
	}
	return out
}

var taskTokenRe = regexp.MustCompile(`\s+(id|src|due|s|pri|parent|blocks|rec|discovered|completed):[^\s]+`)

// cleanTaskText strips todo.txt bookkeeping key:value tokens for display,
// keeping the human-readable description (and +project/@tag/[[link]] words).
func cleanTaskText(text string) string {
	return strings.TrimSpace(taskTokenRe.ReplaceAllString(" "+text, ""))
}
