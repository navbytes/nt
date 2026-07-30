package note

import (
	"fmt"
	"regexp"
	"strings"
)

// The `## Decisions` convention (memory-dynamics spec §5): a note's coarse,
// always-visible change history — one dated bullet per DECISION (not per
// edit), newest first, living in the ordinary markdown body so every read/
// render/search path works on it unchanged. The per-edit fine channel stays
// in git (`nt history`). Chapter granularity on purpose: a one-line "why"
// per revision recovers most of the value of full history at a fraction of
// the read cost.

// DecisionsHeading is the exact section heading the appender maintains.
const DecisionsHeading = "## Decisions"

var decisionLine = regexp.MustCompile(`(?m)^- (\d{4}-\d{2}-\d{2}): `)

// AppendDecision prepends a dated decision bullet to n's ## Decisions section,
// creating the section at the end of the body if it's missing. text must be a
// single line (hostile-input stance shared with invalidFrontmatterLine: a
// newline or a frontmatter delimiter could smuggle structure into the file).
// Mutates n.Body only — the caller owns Save and undo journaling.
func AppendDecision(n *Note, date, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("decision text is required")
	}
	if strings.ContainsAny(text, "\n\r") || text == fmDelim || strings.HasPrefix(text, "#") {
		return fmt.Errorf("decision text must be a single plain line (no newlines, headings, or %q)", fmDelim)
	}
	bullet := "- " + date + ": " + text
	body := n.Body
	if i := decisionsHeadingIndex(body); i >= 0 {
		// Insert the bullet right after the heading line (newest first).
		lineEnd := i + strings.IndexByte(body[i:], '\n')
		if lineEnd < i { // heading is the last line, no trailing newline
			n.Body = body + "\n\n" + bullet + "\n"
			return nil
		}
		rest := body[lineEnd+1:]
		// Skip a single blank spacer line so the list stays attached to any
		// existing bullets while tolerating both spaced and tight styles.
		spacer := ""
		if strings.HasPrefix(rest, "\n") {
			spacer = "\n"
			rest = rest[1:]
		}
		n.Body = body[:lineEnd+1] + spacer + bullet + "\n" + rest
		return nil
	}
	b := strings.TrimRight(body, "\n")
	if b != "" {
		b += "\n\n"
	}
	n.Body = b + DecisionsHeading + "\n\n" + bullet + "\n"
	return nil
}

// decisionsHeadingIndex returns the byte offset of the ## Decisions heading
// line in body, or -1. Exact-heading match at line start only — "## Decisions
// we regret" is someone's prose, not the convention section.
func decisionsHeadingIndex(body string) int {
	for i := 0; i >= 0 && i < len(body); {
		lineEnd := strings.IndexByte(body[i:], '\n')
		var line string
		if lineEnd < 0 {
			line = body[i:]
			lineEnd = len(body) - i
		} else {
			line = body[i : i+lineEnd]
		}
		if strings.TrimRight(line, " \t") == DecisionsHeading {
			return i
		}
		i += lineEnd + 1
	}
	return -1
}

// DecisionStats reports the decision-log summary read paths surface on stubs:
// how many dated bullets the ## Decisions section holds and the newest date
// among them (bullets are newest-first by convention, but this scans all of
// them so a hand-edited section still reports honestly).
func (n *Note) DecisionStats() (count int, latest string) {
	i := decisionsHeadingIndex(n.Body)
	if i < 0 {
		return 0, ""
	}
	section := n.Body[i:]
	// The section ends at the next heading, if any.
	if j := strings.Index(section[len(DecisionsHeading):], "\n#"); j >= 0 {
		section = section[:len(DecisionsHeading)+j]
	}
	for _, m := range decisionLine.FindAllStringSubmatch(section, -1) {
		count++
		if m[1] > latest {
			latest = m[1]
		}
	}
	return count, latest
}
