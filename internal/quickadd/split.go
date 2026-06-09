package quickadd

import (
	"strings"
	"unicode/utf8"
)

// LongCaptureThreshold is the rune count past which a captured task is treated
// as paragraph-length — the "agent dumped its reasoning into one line" case the
// UX audit flagged. Normal tasks are well under this.
const LongCaptureThreshold = 180

// SplitLong decides whether a captured task's text is paragraph-length and
// should be split into a short, actionable task title plus a note body holding
// the full text. It returns the short title, the body (the original text), and
// true when text exceeds the threshold; otherwise "", "", false. It is pure —
// the caller creates the note and links the task.
func SplitLong(text string) (title, body string, ok bool) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= LongCaptureThreshold {
		return "", "", false
	}
	return clauseTitle(text, 72), text, true
}

// clauseTitle reduces long prose to a clean one-line title: it prefers the first
// sentence/clause boundary, else cuts at the last word boundary within max. No
// ellipsis — this becomes a real task and note title, not a display truncation.
func clauseTitle(text string, max int) string {
	t := strings.Join(strings.Fields(text), " ") // collapse whitespace
	if utf8.RuneCountInString(t) <= max {
		return t
	}
	window := firstRunes(t, max+1)
	if i := strings.IndexAny(window, ".?!;:"); i >= 12 {
		return strings.TrimSpace(window[:i])
	}
	head := firstRunes(t, max)
	if cut := strings.LastIndexByte(head, ' '); cut >= 12 {
		return strings.TrimSpace(head[:cut])
	}
	return strings.TrimSpace(head)
}

// firstRunes returns the first n runes of s (byte-correct for UTF-8).
func firstRunes(s string, n int) string {
	i, count := 0, 0
	for i < len(s) && count < n {
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		count++
	}
	return s[:i]
}
