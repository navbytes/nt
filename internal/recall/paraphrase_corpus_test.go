package recall

import (
	"fmt"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

// A paraphrase corpus with a measured HIT@1 floor.
//
// Why it exists: a field test found HIT@1 on a fixed query set fell from 65% to
// 41% purely as the store grew from 20 to 30 notes — nothing would have caught
// that, and there was no way to tell whether a scoring change (length
// normalization, a new synonym group) helped or hurt. recall_precision_test.go
// uses 2-3 note corpora, where the ranking failure modes that matter are
// invisible: a long note winning on coincidental overlap, or a glossary note
// acting as a universal attractor.
//
// Each query is worded to share no verbatim content word with its target.
func corpusNotes() []*note.Note {
	n := func(title, body string, tags ...string) *note.Note { return mk(title, body, tags...) }
	notes := []*note.Note{
		n("Trash overwrites an identically named file", "Deleting the same path twice leaves only the second copy; the first is unrecoverable.", "lesson"),
		n("Archive applies before it validates its input", "A bad handle late in the list still leaves earlier notes retired.", "lesson"),
		n("Nested Go module escapes the vulnerability scan", "Tooling run from the repo root never reaches it, so advisories go unnoticed.", "lesson"),
		n("Concurrent writers time out behind the store lock", "Twenty-five goroutines serialize; the tail exceeds the default budget on slow hardware.", "lesson"),
		n("A slash in a title is read as a folder", "The leading token is consumed as a directory and vanishes from the heading.", "lesson"),
		n("Backticks in a one-line summary are expanded by the shell", "The value silently truncates and the command still reports success.", "lesson"),
		n("Short identifiers in double brackets resolve one way only", "The reverse index misses them, so the target looks unreferenced.", "lesson"),
		n("Renaming pins the previous heading into metadata", "The stored heading then beats the one in the document text.", "lesson"),
		// Distractors: plausible neighbours, plus a glossary note (a known
		// universal attractor because it enumerates everyone else's terms).
		n("Weekly planning", "Review the board, update estimates, groom the queue, and reschedule anything stale.", "note"),
		n("Vocabulary of the ranking layer", "Terms in use: clobber, overwrite, duplicate, lock, timeout, folder, heading, shell, bracket, metadata, scanner, module.", "ref"),
		n("Editor setup", "Fonts, themes, key bindings and pane layout for the terminal.", "note"),
		n("Release checklist", "Tag, build the artefacts, publish notes, announce.", "note"),
	}
	return notes
}

var paraphraseCases = []struct{ query, want string }{
	{"deleting something twice destroys the earlier copy", "Trash overwrites an identically named file"},
	{"a bad name partway through still retires the earlier ones", "Archive applies before it validates its input"},
	{"advisories are missed for the sub-project", "Nested Go module escapes the vulnerability scan"},
	{"many simultaneous writers exceed the wait budget", "Concurrent writers time out behind the store lock"},
	{"the first word of my heading disappeared into a directory", "A slash in a title is read as a folder"},
	{"my summary got cut off because of quoting", "Backticks in a one-line summary are expanded by the shell"},
	{"the target claims nothing points at it", "Short identifiers in double brackets resolve one way only"},
	{"the stored heading disagrees with the document", "Renaming pins the previous heading into metadata"},
}

// minHitAtOne is the floor this corpus must clear. Raise it when a scoring
// change genuinely improves the number — never lower it to make a change pass.
const minHitAtOne = 8

func TestParaphraseCorpusHitAtOne(t *testing.T) {
	notes := corpusNotes()
	hits, misses := 0, []string{}
	for _, c := range paraphraseCases {
		got := Rank(notes, c.query, 5)
		if len(got) > 0 && got[0].Note.Title == c.want {
			hits++
			continue
		}
		top := "(nothing)"
		if len(got) > 0 {
			top = got[0].Note.Title
		}
		misses = append(misses, fmt.Sprintf("%q\n      want %q\n      got  %q", c.query, c.want, top))
	}
	t.Logf("HIT@1 = %d/%d", hits, len(paraphraseCases))
	if hits < minHitAtOne {
		for _, m := range misses {
			t.Errorf("miss: %s", m)
		}
		t.Fatalf("HIT@1 %d/%d is below the floor of %d", hits, len(paraphraseCases), minHitAtOne)
	}
}

// The glossary note enumerates other notes' vocabulary, which made it a
// universal attractor in the field test — it out-ranked the specific lessons
// about its own terms. It must never take #1 on a query aimed elsewhere.
func TestGlossaryNoteIsNotAUniversalAttractor(t *testing.T) {
	notes := corpusNotes()
	for _, c := range paraphraseCases {
		got := Rank(notes, c.query, 5)
		if len(got) > 0 && got[0].Note.Title == "Vocabulary of the ranking layer" {
			t.Errorf("glossary note took #1 for %q", c.query)
		}
	}
}
