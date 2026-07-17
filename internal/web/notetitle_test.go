package web

import (
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/web/apitypes"
)

// Regression (mirrors the CLI shorthand fix): POST /api/notes must not split a
// prose title containing or ending with a slash into folder + title — before
// the shared SplitPathTitle boundary, this title 400'd on the folder allowlist
// (its bogus "folder" contained '.').
func TestAPINoteCreateProseSlashTitle(t *testing.T) {
	s := newTestServer(t)
	title := "Design valid at .claude/company/release-2.0-search/"
	code, body := postForm(s, "/api/notes", s.csrf, mustValues("title", title))
	if code != 200 {
		t.Fatalf("create: %d %s", code, body)
	}
	if res := decode[apitypes.CreatedNote](t, body); res.Handle == "" || res.URL == "" {
		t.Fatalf("create response missing handle/url: %+v", res)
	}
	notes, _ := note.List(s.eng.S)
	var found *note.Note
	for _, n := range notes {
		if n.Title == title {
			found = n
		}
	}
	if found == nil {
		t.Fatal("note with intact title not found")
	}
	if strings.Contains(found.Rel, "/") {
		t.Errorf("prose title was filed into a folder: rel %q", found.Rel)
	}
}
