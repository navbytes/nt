package aisync

import (
	"testing"

	"github.com/navbytes/nt/internal/mutate"
)

func engine(t *testing.T) *mutate.Engine {
	t.Setenv("NT_DIR", t.TempDir())
	e, err := mutate.Open()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func tasks(t *testing.T, e *mutate.Engine) []string {
	d, err := e.Read()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tk := range d.Tasks() {
		out = append(out, tk.Status()+":"+tk.Text+":"+tk.Source())
	}
	return out
}

func TestSyncUpsertsIdempotently(t *testing.T) {
	e := engine(t)
	p1 := `{"session_id":"s1","tool_name":"TodoWrite","tool_input":{"todos":[
		{"id":"1","content":"implement auth","status":"in_progress"},
		{"id":"2","content":"write tests","status":"pending"}]}}`
	if err := Sync(e, []byte(p1)); err != nil {
		t.Fatal(err)
	}
	if got := tasks(t, e); len(got) != 2 {
		t.Fatalf("want 2 tasks, got %d: %v", len(got), got)
	}

	// Second write: todo 1 completed, todo 3 added, todo 2 unchanged.
	p2 := `{"session_id":"s1","tool_name":"TodoWrite","tool_input":{"todos":[
		{"id":"1","content":"implement auth","status":"completed"},
		{"id":"2","content":"write tests","status":"pending"},
		{"id":"3","content":"deploy","status":"in_progress"}]}}`
	if err := Sync(e, []byte(p2)); err != nil {
		t.Fatal(err)
	}
	got := tasks(t, e)
	if len(got) != 3 {
		t.Fatalf("want 3 tasks after second sync (no dupes), got %d: %v", len(got), got)
	}
	var done, doing, open int
	for _, s := range got {
		switch {
		case s == "done:implement auth:claude":
			done++
		case s == "doing:deploy:claude":
			doing++
		case s == "open:write tests:claude":
			open++
		}
	}
	if done != 1 || doing != 1 || open != 1 {
		t.Fatalf("unexpected statuses: %v", got)
	}
}

func TestSyncIgnoresNonTodoWrite(t *testing.T) {
	e := engine(t)
	if err := Sync(e, []byte(`{"tool_name":"Bash","tool_input":{}}`)); err != nil {
		t.Fatal(err)
	}
	if got := tasks(t, e); len(got) != 0 {
		t.Fatalf("non-TodoWrite event should add nothing, got %v", got)
	}
	// Malformed JSON must not error.
	if err := Sync(e, []byte(`not json`)); err != nil {
		t.Fatalf("malformed input should be ignored, got %v", err)
	}
}
