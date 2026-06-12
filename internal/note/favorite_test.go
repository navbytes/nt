package note

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteRoundTrips(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Cheat Sheet", "the commands I always forget", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.Favorite {
		t.Fatal("a new note should not be a favorite")
	}

	n.Favorite = true
	if err := n.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Favorite {
		t.Error("favorite: true should round-trip through frontmatter")
	}
	if got.Body == "" || got.Title != "Cheat Sheet" {
		t.Errorf("favoriting must not disturb the rest of the note: %+v", got)
	}
}

// A note can be both archived and favorited — the two flags are orthogonal and
// must both survive a save/load cycle independently.
func TestFavoriteAndArchiveCoexist(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Both", "starred but retired", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	n.Favorite = true
	n.Archived = true
	if err := n.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Favorite || !got.Archived {
		t.Errorf("favorite and archived should both round-trip, got %+v", got)
	}
	// Unfavoriting drops only the favorite line, leaving archived intact.
	got.Favorite = false
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	rawBytes, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(rawBytes)
	if strings.Contains(raw, "favorite:") {
		t.Errorf("unfavoriting should remove the favorite line:\n%s", raw)
	}
	if !strings.Contains(raw, "archived: true") {
		t.Errorf("unfavoriting must not touch archived:\n%s", raw)
	}
}
