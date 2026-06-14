package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/web/apitypes"
)

func deleteReq(s *Server, path, csrf string) (int, string) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", path, nil)
	if csrf != "" {
		r.Header.Set("X-CSRF", csrf)
	}
	s.routes().ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// TestAPINoteDelete covers the web note-delete flow: edit/CSRF gating, the
// 409 guard when inbound links would dangle, mode=unlink (strips them, deletes),
// and a clean delete for an unlinked note.
func TestAPINoteDelete(t *testing.T) {
	s := newTestServer(t)

	target, _ := note.Create(s.eng.S, "Target", "the target", nil, "test", "ref")
	_, _ = note.Create(s.eng.S, "Referrer", "see [[Target]] here", nil, "test", "ref")
	s.rebuild()
	th := noteHandle(target)

	// Read-only: refused.
	if code, _ := deleteReq(s, "/api/notes/"+th, s.csrf); code != 403 {
		t.Fatalf("delete without --edit should be 403, got %d", code)
	}
	s.allowEdit = true

	// Missing CSRF: refused.
	if code, _ := deleteReq(s, "/api/notes/"+th, ""); code != 403 {
		t.Fatalf("delete without CSRF should be 403, got %d", code)
	}

	// Inbound link present, no mode → 409 (guarded, not silently dangling).
	if code, _ := deleteReq(s, "/api/notes/"+th, s.csrf); code != 409 {
		t.Fatalf("linked note delete without a mode should be 409, got %d", code)
	}

	// mode=unlink strips the reference, then trashes the note.
	code, body := deleteReq(s, "/api/notes/"+th+"?mode=unlink", s.csrf)
	if code != 200 {
		t.Fatalf("unlink delete should succeed, got %d: %s", code, body)
	}
	res := decode[apitypes.DeletedNote](t, body)
	if res.Unlinked != 1 {
		t.Fatalf("expected 1 reference unlinked, got %d", res.Unlinked)
	}
	notes, _ := note.List(s.eng.S)
	for _, n := range notes {
		if n.Title == "Target" {
			t.Fatal("Target should be trashed")
		}
		if n.Title == "Referrer" && strings.Contains(n.Body, "[[Target]]") {
			t.Fatalf("inbound link should be stripped: %q", n.Body)
		}
	}

	// An unlinked note deletes cleanly with no mode.
	lonely, _ := note.Create(s.eng.S, "Lonely", "nothing links here", nil, "test", "ref")
	s.rebuild()
	if code, body := deleteReq(s, "/api/notes/"+noteHandle(lonely), s.csrf); code != 200 {
		t.Fatalf("unlinked note delete should succeed, got %d: %s", code, body)
	}
}
