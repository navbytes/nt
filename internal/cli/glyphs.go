package cli

import (
	"os"
	"strings"
)

// asciiMode reports whether the user has opted into plain ASCII output instead
// of the default unicode glyphs. It is gated on environment ONLY (never TTY
// detection) so that piping or capturing output never silently changes the
// glyphs, and tests that read into buffers keep seeing unicode by default.
//
// It triggers when either NT_ASCII is set to a truthy value (1/true/yes) or
// NO_COLOR is set to any value (per the no-color.org convention of asking tools
// for plainer output). With both unset, output is unchanged unicode.
func asciiMode() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NT_ASCII"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// glyph returns unicode in the default mode and ascii when asciiMode() is on.
// Centralizing the switch here keeps every call site readable and makes the
// unicode/ASCII pairs flip together.
func glyph(unicode, ascii string) string {
	if asciiMode() {
		return ascii
	}
	return unicode
}

// iconStatus maps an effective task status to its list-row status icon.
// Unicode is single-width; the ASCII forms are the conventional checkbox marks
// and stay within the same fixed column the format strings expect.
func iconStatus(status string) string {
	switch status {
	case "done":
		return glyph("✓", "[x]")
	case "doing":
		return glyph("◐", "[~]")
	case "blocked":
		return glyph("⊘", "[!]")
	default: // open
		return glyph("○", "[ ]")
	}
}

// Relationship / marker glyphs used in reports and recall output.
func glyphNote() string        { return glyph("▤", "-") }   // note marker
func glyphSubtaskOf() string   { return glyph("⤴", "^") }   // subtask of
func glyphChild() string       { return glyph("⤵", ">") }   // child / subtask
func glyphBlocks() string      { return glyph("⛔", "(x)") } // this task blocks
func glyphBlockedBy() string   { return glyph("⏳", "(.)") } // waiting / blocked by
func glyphDiscFrom() string    { return glyph("↑", "<-") }  // discovered from
func glyphDiscHere() string    { return glyph("↳", "->") }  // discovered here (other)
func glyphDone() string        { return glyph("✓", "[x]") } // completed (logbook)
func glyphReviewClear() string { return glyph("✨", "(done)") }
