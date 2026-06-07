package mutate

import (
	"os"
	"testing"

	"github.com/navbytes/nt/internal/store"
)

func TestDoctorDedupAndAssign(t *testing.T) {
	e := newEngine(t)

	// Simulate a post-union-merge tasks.txt: the same id appears twice (open +
	// done), plus a hand-written line with no id.
	const raw = "fix auth bug id:01AAAAAAAAAAAAAAAAAAAAAAAA\n" +
		"x 2026-06-07 fix auth bug id:01AAAAAAAAAAAAAAAAAAAAAAAA\n" +
		"hand written task with no id\n"
	if err := store.WriteAtomic(e.S.TasksFile(), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	// Dry run reports but doesn't change the file.
	rep, err := e.Doctor(false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DupIDsRemoved != 1 || rep.IDsAssigned != 1 {
		t.Fatalf("dry-run report: got dup=%d assign=%d, want 1/1", rep.DupIDsRemoved, rep.IDsAssigned)
	}
	if got, _ := os.ReadFile(e.S.TasksFile()); string(got) != raw {
		t.Fatal("dry run must not modify the file")
	}

	// Apply: dedup + assign, and persist.
	if _, err := e.Doctor(true); err != nil {
		t.Fatal(err)
	}
	ts := tasks(t, e)
	if len(ts) != 2 {
		t.Fatalf("after doctor: want 2 tasks, got %d", len(ts))
	}
	// The surviving 01AAA… line must be the DONE one (completion isn't lost).
	var kept bool
	for _, tk := range ts {
		if tk.ID() == "01AAAAAAAAAAAAAAAAAAAAAAAA" {
			kept = true
			if !tk.Done {
				t.Error("dedup should keep the completed line, not the open one")
			}
		}
	}
	if !kept {
		t.Error("the deduped id should still be present once")
	}
	// The id-less line now has an id.
	for _, tk := range ts {
		if tk.Text == "hand written task with no id" && tk.ID() == "" {
			t.Error("doctor should have assigned an id to the id-less task")
		}
	}

	// Idempotent: a second run finds nothing.
	if rep2, _ := e.Doctor(true); rep2.Issues() != 0 {
		t.Errorf("doctor should be idempotent, second run found %d issues", rep2.Issues())
	}
}
