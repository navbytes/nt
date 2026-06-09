package note

import "testing"

func TestArchivedRoundTrips(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Old Decision", "the reasoning behind X", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.Archived {
		t.Fatal("a new note should not be archived")
	}

	n.Archived = true
	if err := n.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Archived {
		t.Error("archived: true should round-trip through frontmatter")
	}
	if got.Body == "" || got.Title != "Old Decision" {
		t.Errorf("archiving must not disturb the rest of the note: %+v", got)
	}
}

func TestActiveDropsArchived(t *testing.T) {
	ns := []*Note{{Title: "live"}, {Title: "retired", Archived: true}, {Title: "also live"}}
	got := Active(ns)
	if len(got) != 2 || got[0].Title != "live" || got[1].Title != "also live" {
		t.Errorf("Active should drop archived notes, got %d: %+v", len(got), got)
	}
}
