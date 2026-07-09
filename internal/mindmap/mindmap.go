// Package mindmap turns a note's Markdown outline (its headings and nested list
// items) into a Mermaid `mindmap` diagram. It's the terminal/agent counterpart
// to the web app's interactive mind map: `nt mindmap <note>` (and the MCP
// nt_mindmap tool) emit a ```mermaid``` fence an agent can drop straight into a
// note — where nt's own web viewer, Obsidian, or GitHub render it.
//
// The parser is a deliberately small line scanner (headings + lists, skipping
// fenced code) rather than a full Markdown AST: notes are simple, the mapping is
// the markmap model (H1 trunk → headings branches → bullets leaves), and a pure
// function is trivially unit-tested.
package mindmap

import (
	"regexp"
	"strings"
)

// Node is one entry in the outline tree (the root carries the note title).
type Node struct {
	Text     string  `json:"text"`
	Children []*Node `json:"children,omitempty"`
}

// MaxNodes bounds a single map so a pathological note can't produce a runaway
// diagram (mirrors the web view's cap).
const MaxNodes = 400

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	// A bullet ("-", "*", "+") or ordered ("1.", "2)") list marker, capturing the
	// leading indent so nesting can be derived from it.
	listRe = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.+?)\s*$`)
	// Inline Markdown we strip so node labels read cleanly in the diagram.
	wikiAliasRe = regexp.MustCompile(`\[\[([^\]|]+)\|([^\]]+)\]\]`) // [[target|alias]] → alias
	wikiRe      = regexp.MustCompile(`\[\[([^\]]+)\]\]`)            // [[target]] → target
	mdLinkRe    = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)       // [text](url) → text
	emphasisRe  = regexp.MustCompile("[*_`~]")                      // ** __ ` ~~
	bracketsRe  = regexp.MustCompile(`[()\[\]{}]`)                  // mindmap shape delimiters
	wsRe        = regexp.MustCompile(`\s+`)
)

// Outline parses a note body into a tree rooted at the title. Headings nest by
// level; a list under a heading (or before the first heading, under the root)
// contributes its items, with sub-lists nested by indentation. Fenced code
// blocks and plain paragraphs are ignored — a mind map is the skeleton.
func Outline(body, title string) *Node {
	root := &Node{Text: firstNonEmpty(sanitize(title), "(untitled)")}
	count := 1
	// Drop a leading "# <title>" H1 that just echoes the note title (nt prepends
	// one for bodies authored without a heading) — otherwise the map would show
	// the title twice: once as the root, once as a child. Mirrors the web
	// renderer's stripTitleH1 so all three surfaces agree.
	body = stripTitleH1(body, title)

	type hframe struct {
		node  *Node
		level int
	}
	type lframe struct {
		node   *Node
		indent int
	}
	hstack := []hframe{{root, 0}}
	var lstack []lframe

	inFence := false
	fence := ""

	pushHeading := func(level int, text string) {
		for len(hstack) > 1 && hstack[len(hstack)-1].level >= level {
			hstack = hstack[:len(hstack)-1]
		}
		n := &Node{Text: sanitize(text)}
		hstack[len(hstack)-1].node.Children = append(hstack[len(hstack)-1].node.Children, n)
		hstack = append(hstack, hframe{n, level})
		lstack = nil // headings reset the list context
		count++
	}

	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)

		// Fenced code blocks toggle on ``` or ~~~ and swallow everything between.
		if m := fenceMarker(trimmed); m != "" {
			if !inFence {
				inFence, fence = true, m
			} else if strings.HasPrefix(trimmed, fence) {
				inFence = false
			}
			continue
		}
		// A blank line does NOT end a list run (CommonMark loose lists keep nesting
		// across blanks, and goldmark's nested <ul> agrees) — only a heading or a
		// genuine prose paragraph resets the list context.
		if inFence || trimmed == "" {
			continue
		}
		if count >= MaxNodes {
			break
		}

		if m := headingRe.FindStringSubmatch(trimmed); m != nil {
			pushHeading(len(m[1]), m[2])
			continue
		}

		if m := listRe.FindStringSubmatch(raw); m != nil {
			indent := indentWidth(m[1])
			for len(lstack) > 0 && lstack[len(lstack)-1].indent >= indent {
				lstack = lstack[:len(lstack)-1]
			}
			var parent *Node
			if len(lstack) == 0 {
				parent = hstack[len(hstack)-1].node
			} else {
				parent = lstack[len(lstack)-1].node
			}
			n := &Node{Text: sanitize(m[2])}
			parent.Children = append(parent.Children, n)
			lstack = append(lstack, lframe{n, indent})
			count++
			continue
		}

		// Setext heading: a prose line underlined by === (H1) or --- (H2).
		if len(lstack) == 0 && i+1 < len(lines) {
			if lvl := setextLevel(lines[i+1]); lvl != 0 {
				pushHeading(lvl, trimmed)
				i++ // consume the underline
				continue
			}
		}

		// A genuine prose paragraph ends the current list run.
		lstack = nil
	}
	return root
}

// Mermaid renders the tree as a Mermaid `mindmap` body (no code fence). maxDepth
// limits how many levels below the root are emitted (0 = all). Indentation is
// two spaces per level, which is what mindmap uses to infer hierarchy.
func Mermaid(root *Node, maxDepth int) string {
	var b strings.Builder
	b.WriteString("mindmap\n")
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		indent := strings.Repeat("  ", depth+1)
		text := firstNonEmpty(n.Text, "•")
		if depth == 0 {
			b.WriteString(indent + "root((" + text + "))\n")
		} else {
			b.WriteString(indent + text + "\n")
		}
		if maxDepth > 0 && depth >= maxDepth {
			return
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return b.String()
}

// Fence wraps a Mermaid body in a ```mermaid``` code block, so the output pastes
// straight into a note and renders in nt's web viewer / Obsidian / GitHub.
func Fence(mermaid string) string {
	return "```mermaid\n" + strings.TrimRight(mermaid, "\n") + "\n```\n"
}

// sanitize turns raw Markdown label text into a clean, mindmap-safe single line:
// wikilinks/links collapse to their text, emphasis/backticks drop, and the shape
// delimiters mindmap reserves ( () [] {} ) are removed so a label can't be
// misread as a node shape.
func sanitize(s string) string {
	s = wikiAliasRe.ReplaceAllString(s, "$2")
	s = wikiRe.ReplaceAllString(s, "$1")
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = emphasisRe.ReplaceAllString(s, "")
	s = bracketsRe.ReplaceAllString(s, " ")
	s = wsRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	const max = 80
	if len(s) > max {
		s = strings.TrimSpace(s[:max-1]) + "…"
	}
	return s
}

// stripTitleH1 removes a leading "# <title>" line when it duplicates the note
// title (case- and whitespace-insensitively), leaving a genuinely different
// first heading untouched. Ported from the web renderer for cross-surface parity.
func stripTitleH1(body, title string) string {
	s := strings.TrimLeft(body, "\n")
	first, rest, _ := strings.Cut(s, "\n")
	h := strings.TrimSpace(first)
	if !strings.HasPrefix(h, "# ") {
		return body
	}
	if !strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(h, "#")), strings.TrimSpace(title)) {
		return body
	}
	return strings.TrimLeft(rest, "\n")
}

var (
	setextH1Re = regexp.MustCompile(`^=+$`)
	setextH2Re = regexp.MustCompile(`^-+$`)
)

// setextLevel returns 1 for an "===" underline, 2 for "---", else 0.
func setextLevel(line string) int {
	t := strings.TrimSpace(line)
	switch {
	case setextH1Re.MatchString(t):
		return 1
	case setextH2Re.MatchString(t):
		return 2
	}
	return 0
}

// fenceMarker returns "```" or "~~~" if the line opens/closes a code fence.
func fenceMarker(trimmed string) string {
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```"
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~"
	}
	return ""
}

// indentWidth counts leading whitespace, treating a tab as two columns so tab-
// and space-indented lists nest consistently.
func indentWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\t' {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
