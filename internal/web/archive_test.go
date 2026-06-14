package web

import (
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/web/apitypes"
)

// TestAPINoteArchive walks the web archive lifecycle: gating, then archive →
// dropped from search/sidebar/state but still reachable from the grid (flagged)
// and its own page, then unarchive → restored, then a no-field toggle.
func TestAPINoteArchive(t *testing.T) {
	s := newTestServer(t)
	target, _ := note.Create(s.eng.S, "Zephyr", "# Zephyr\n\nquaxified marker text", nil, "cli", "")
	note.Create(s.eng.S, "Linker", "points at [[Zephyr]]", nil, "cli", "")

	searchHasMarker := func() bool {
		_, body := get(t, s, "/api/search?q=quaxified")
		return len(decode[apitypes.SearchResponse](t, body).Results) > 0
	}

	// Gating: a write without the CSRF token is refused.
	if code, _ := postForm(s, "/api/notes/"+target.ID+"/archive", "", mustValues("archived", "true")); code != 403 {
		t.Errorf("archive should 403 without CSRF, got %d", code)
	}

	if !searchHasMarker() {
		t.Fatal("precondition: the marker should be searchable before archiving")
	}

	// Archive it.
	code, body := postForm(s, "/api/notes/"+target.ID+"/archive", s.csrf, mustValues("archived", "true"))
	if code != 200 {
		t.Fatalf("archive: %d %s", code, body)
	}
	if res := decode[apitypes.ArchivedNote](t, body); !res.Archived || res.Handle != target.ID {
		t.Errorf("archive result wrong: %+v", res)
	}

	// Dropped from search, the sidebar/⌘K index, and the active note count.
	if searchHasMarker() {
		t.Error("an archived note should drop out of search")
	}
	_, body = get(t, s, "/api/notes")
	if idx := decode[apitypes.NotesIndex](t, body); len(idx.Index) != 1 {
		t.Errorf("sidebar index should hold only the Linker, got %d", len(idx.Index))
	}
	_, body = get(t, s, "/api/state")
	if st := decode[apitypes.State](t, body); st.NoteCount != 1 {
		t.Errorf("noteCount should drop to the active 1, got %d", st.NoteCount)
	}

	// Still listed in the grid, flagged Archived (so the toggle can reveal it).
	_, body = get(t, s, "/api/notes/grid")
	grid := decode[apitypes.NotesGrid](t, body)
	var card *apitypes.NoteCard
	for i := range grid.Notes {
		if grid.Notes[i].Title == "Zephyr" {
			card = &grid.Notes[i]
		}
	}
	if card == nil || !card.Archived {
		t.Errorf("grid should still list Zephyr with Archived=true, got %+v", card)
	}

	// The note page itself is still reachable, flagged so it can show "Unarchive".
	_, body = get(t, s, "/api/notes/"+target.ID)
	if nv := decode[apitypes.NoteView](t, body); !nv.Archived || nv.Title != "Zephyr" {
		t.Errorf("note view should be reachable and Archived, got %+v", nv)
	}

	// Unarchive restores it to search.
	if code, body = postForm(s, "/api/notes/"+target.ID+"/archive", s.csrf, mustValues("archived", "false")); code != 200 {
		t.Fatalf("unarchive: %d %s", code, body)
	}
	if !searchHasMarker() {
		t.Error("an unarchived note should return to search")
	}

	// No explicit field → toggles (active → archived).
	code, body = postForm(s, "/api/notes/"+target.ID+"/archive", s.csrf, nil)
	if code != 200 {
		t.Fatalf("toggle: %d %s", code, body)
	}
	if r := decode[apitypes.ArchivedNote](t, body); !r.Archived {
		t.Errorf("a no-field archive should toggle active→archived, got %+v", r)
	}
}

// TestAPIArchiveDropsOrphan guards the orphan accounting: an archived note is
// out of the link graph, so it must not be reported as an orphan (it has no
// links only because it's retired, not because it needs attention).
func TestAPIArchiveDropsOrphan(t *testing.T) {
	s := newTestServer(t)
	lonely, _ := note.Create(s.eng.S, "Lonely", "no links here", nil, "cli", "")

	_, body := get(t, s, "/api/orphans")
	if o := decode[apitypes.OrphansResponse](t, body); len(o.Notes) != 1 {
		t.Fatalf("Lonely should be an orphan before archiving, got %+v", o.Notes)
	}
	if code, _ := postForm(s, "/api/notes/"+lonely.ID+"/archive", s.csrf, mustValues("archived", "true")); code != 200 {
		t.Fatal("archive failed")
	}
	_, body = get(t, s, "/api/orphans")
	if o := decode[apitypes.OrphansResponse](t, body); len(o.Notes) != 0 {
		t.Errorf("an archived note must not surface as an orphan, got %+v", o.Notes)
	}
}
