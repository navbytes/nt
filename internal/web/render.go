package web

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/note"
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
	// Syntax-highlight fenced code with a known language (Chroma — already in the
	// module graph via Glamour). Emits classed spans; colors come from the
	// theme-scoped CSS at /static/highlight.css.
	out = highlightCode(out)
	return template.HTML(out), nil //nolint:gosec // goldmark output + Chroma classes are escaped
}

var (
	mermaidBlockRe = regexp.MustCompile(`(?s)<pre><code class="language-mermaid">(.*?)</code></pre>`)
	wikiClassRe    = regexp.MustCompile(`<a href="(/n/[^"?]+)">`)
	codeBlockRe    = regexp.MustCompile(`(?s)<pre><code class="language-([\w+#.-]+)">(.*?)</code></pre>`)
	bgColorRe      = regexp.MustCompile(`background-color:[^;}]*;?`)
)

// chromaFormatter emits class names (not inline styles), so one render serves
// both light and dark — the colors live in theme-scoped CSS.
var chromaFormatter = chromahtml.New(chromahtml.WithClasses(true))

// highlightCode replaces ```lang fenced blocks with Chroma-highlighted markup.
// Unknown languages and plain ``` blocks are left as goldmark emitted them.
func highlightCode(s string) string {
	return codeBlockRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := codeBlockRe.FindStringSubmatch(m)
		lexer := lexers.Get(sub[1])
		if lexer == nil {
			return m
		}
		it, err := lexer.Tokenise(nil, html.UnescapeString(sub[2]))
		if err != nil {
			return m
		}
		var buf bytes.Buffer
		if err := chromaFormatter.Format(&buf, styles.Fallback, it); err != nil {
			return m
		}
		return buf.String()
	})
}

// highlightCSS builds the theme-scoped stylesheet for the Chroma classes: a light
// palette by default, a dark one under both prefers-color-scheme and the manual
// [data-theme="dark"] toggle.
func highlightCSS() string {
	light := styles.Get("github")
	dark := styles.Get("github-dark")
	if dark == nil {
		dark = styles.Fallback
	}
	var lb, db bytes.Buffer
	_ = chromaFormatter.WriteCSS(&lb, light)
	_ = chromaFormatter.WriteCSS(&db, dark)
	// Drop Chroma's own backgrounds so code blocks keep nt's --bg-inset surface
	// (consistent with inline code); only the token colors are themed.
	lightCSS := bgColorRe.ReplaceAllString(lb.String(), "")
	darkCSS := bgColorRe.ReplaceAllString(db.String(), "")
	var b strings.Builder
	b.WriteString(lightCSS)
	b.WriteString("@media (prefers-color-scheme: dark){")
	b.WriteString(strings.ReplaceAll(darkCSS, ".chroma", `:root:not([data-theme="light"]) .chroma`))
	b.WriteString("}")
	b.WriteString(strings.ReplaceAll(darkCSS, ".chroma", `[data-theme="dark"] .chroma`))
	return b.String()
}

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
		h := it.ID
		if h == "" { // id-less note (authored outside nt) — route by its path
			for _, n := range notes {
				if n.Path == it.Path {
					h = n.Rel
					break
				}
			}
		}
		return "[" + label + "](/n/" + url.PathEscape(h) + ")"
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

// Note → note backlinks (the "Linked from" panel) and task → note references
// (the "Referenced by tasks" panel) are precomputed once per store change in
// buildSnapshot (readmodel.go), keyed by note path — so a note page is a map
// lookup, not a per-request full-store ripgrep.

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

var taskTokenRe = regexp.MustCompile(`\s+(id|src|due|s|pri|parent|blocks|rec|discovered|completed):[^\s]+`)

// cleanTaskText strips todo.txt bookkeeping key:value tokens for display,
// keeping the human-readable description (and +project/@tag/[[link]] words).
func cleanTaskText(text string) string {
	return strings.TrimSpace(taskTokenRe.ReplaceAllString(" "+text, ""))
}
