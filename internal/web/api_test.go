package web

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/store"
)

func mustValues(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, body)
	}
	return v
}

func TestAPIStateAndNotes(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Alpha", "# Alpha\n\nbody with [[Beta]]", nil, "claude", "")
	note.Create(s.eng.S, "Beta", "b", nil, "cli", "")
	addTask(t, s, "open one")

	_, body := get(t, s, "/api/state")
	st := decode[apiStateDTO](t, body)
	if st.NoteCount != 2 || st.OpenCount != 1 || st.CanEdit {
		t.Fatalf("state wrong: %+v", st)
	}

	_, body = get(t, s, "/api/notes")
	idx := decode[apiNotesDTO](t, body)
	if len(idx.Index) != 2 || len(idx.Tree) == 0 {
		t.Fatalf("notes index wrong: %+v", idx)
	}

	_, body = get(t, s, "/api/notes/"+n.ID)
	nv := decode[apiNoteDTO](t, body)
	if nv.Title != "Alpha" || !strings.Contains(nv.BodyHTML, "wikilink") || nv.ETag == "" {
		t.Fatalf("note view wrong: %+v", nv)
	}
}

func TestAPITasksReadAndWrite(t *testing.T) {
	s := newTestServer(t)
	s.allowEdit = true
	id := addTask(t, s, "ship it")

	_, body := get(t, s, "/api/tasks")
	if g := decode[apiTasksDTO](t, body).Groups; len(g) == 0 {
		t.Fatal("expected task groups")
	}

	// gate: no CSRF → 403
	if code, _ := postForm(s, "/api/tasks/"+id+"/done", "", nil); code != 403 {
		t.Errorf("done without CSRF should 403, got %d", code)
	}
	code, body := postForm(s, "/api/tasks/"+id+"/done", s.csrf, nil)
	if code != 200 {
		t.Fatalf("done: %d", code)
	}
	if !mustDoc(t, s).FindByID(id).Done {
		t.Fatal("task should be done")
	}
	if g := decode[apiTasksDTO](t, body).Groups; len(g) == 0 || g[0].Status == "" {
		t.Fatalf("done response should return groups: %s", body)
	}

	code, body = postForm(s, "/api/tasks", s.csrf, mustValues("text", "new via api", "pri", "high"))
	if code != 200 {
		t.Fatalf("new: %d", code)
	}
	if !strings.Contains(body, "new via api") {
		t.Fatalf("new task not in response: %s", body)
	}
}

func TestAPINoteRawSaveGuard(t *testing.T) {
	s := newTestServer(t)
	n, _ := note.Create(s.eng.S, "Edit Me", "v1", nil, "cli", "")

	// read-only → raw 404
	if resp, _ := get(t, s, "/api/notes/"+n.ID+"/raw"); resp.StatusCode != 404 {
		t.Errorf("raw should 404 read-only, got %d", resp.StatusCode)
	}
	s.allowEdit = true
	_, body := get(t, s, "/api/notes/"+n.ID+"/raw")
	raw := decode[apiRawDTO](t, body)
	if !strings.Contains(raw.Text, "v1") || raw.ETag == "" {
		t.Fatalf("raw wrong: %+v", raw)
	}

	// concurrent write → stale save 409
	if err := store.WriteAtomic(n.Path, []byte("---\nid: "+n.ID+"\n---\n\nagent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/notes/"+n.ID, strings.NewReader("clobber\n"))
	req.Header.Set("X-CSRF", s.csrf)
	req.Header.Set("If-Match", raw.ETag)
	s.routes().ServeHTTP(rec, req)
	if rec.Code != 409 {
		t.Fatalf("stale save should 409, got %d", rec.Code)
	}
}

func TestAPIActivityGraphSearch(t *testing.T) {
	s := newTestServer(t)
	note.Create(s.eng.S, "Hub", "see [[Spoke]]", nil, "claude", "")
	note.Create(s.eng.S, "Spoke", "x", nil, "cli", "")

	_, body := get(t, s, "/api/activity")
	if act := decode[apiActivityDTO](t, body); len(act.Days) == 0 || !contains(act.Sources, "claude") {
		t.Fatalf("activity wrong: %+v", act)
	}
	_, body = get(t, s, "/api/graph")
	g := decode[graphData](t, body)
	if len(g.Nodes) != 2 || len(g.Links) != 1 {
		t.Fatalf("graph wrong: %d nodes %d links", len(g.Nodes), len(g.Links))
	}
	_, body = get(t, s, "/api/search?q=Spoke")
	if r := decode[apiSearchDTO](t, body); len(r.Results) == 0 {
		t.Fatalf("search returned nothing: %s", body)
	}
}
