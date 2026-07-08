package note

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPeekUndoEmptyJournal(t *testing.T) {
	s := testStore(t)
	_, ok, err := PeekUndo(s)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("PeekUndo on an empty/missing journal should report ok=false")
	}
}

func TestApplyReversalRoundTrip(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Undo target", "v1", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the edit an nt_note_edit/cmdEdit call would make + journal.
	if err := os.WriteFile(n.Path, []byte("v2 content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RecordUndo(s, UndoEntry{Op: "edited", TS: time.Now().UTC().Format(time.RFC3339Nano), Path: n.Path, Before: string(before)}); err != nil {
		t.Fatal(err)
	}

	// Undo restores the exact prior bytes.
	path, op, did, err := ApplyReversal(s, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected ApplyReversal to find a pending entry")
	}
	if path != n.Path || op != "edited" {
		t.Errorf("path=%q op=%q, want path=%q op=%q", path, op, n.Path, "edited")
	}
	got, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Errorf("file content after undo = %q, want the original %q", got, before)
	}

	// A second undo (nothing left to undo — top entry is now a redo) is a no-op.
	_, _, did, err = ApplyReversal(s, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if did {
		t.Error("a second plain undo should find nothing eligible (top entry is a redo)")
	}

	// Redo re-applies the edit.
	path, op, did, err = ApplyReversal(s, "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected redo to find the pending redo entry")
	}
	if path != n.Path || op != "edited" {
		t.Errorf("redo path=%q op=%q, want path=%q op=%q", path, op, n.Path, "edited")
	}
	got, err = os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2 content" {
		t.Errorf("file content after redo = %q, want %q", got, "v2 content")
	}
}

func TestApplyReversalRefusesAnotherWorkstreamWithoutForce(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Owned by agent-a", "v1", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordUndo(s, UndoEntry{Op: "edited", TS: time.Now().UTC().Format(time.RFC3339Nano), WS: "agent-a", Path: n.Path, Before: "v1"}); err != nil {
		t.Fatal(err)
	}

	_, _, did, err := ApplyReversal(s, "agent-b", false, false)
	if err == nil {
		t.Fatal("expected an ownership error when reverting another workstream's edit without force")
	}
	if did {
		t.Error("did should be false on refusal")
	}
	if !strings.Contains(err.Error(), "agent-a") {
		t.Errorf("error should name the owning workstream: %v", err)
	}

	// force overrides.
	_, _, did, err = ApplyReversal(s, "agent-b", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected force to allow the reversal")
	}
}

func TestApplyReversalMissingFileErrors(t *testing.T) {
	s := testStore(t)
	if err := RecordUndo(s, UndoEntry{Op: "edited", TS: time.Now().UTC().Format(time.RFC3339Nano), Path: s.NotesDir() + "/gone.md", Before: "v1"}); err != nil {
		t.Fatal(err)
	}
	_, _, did, err := ApplyReversal(s, "", false, false)
	if err == nil {
		t.Fatal("expected an error when the note file no longer exists")
	}
	if did {
		t.Error("did should be false when the revert fails")
	}
}

// The journal compacts past its size threshold, keeping only the most recent
// entries — mirrors internal/undo's task-journal test.
func TestRecordUndoCompactsOversizedJournal(t *testing.T) {
	s := testStore(t)
	padding := strings.Repeat("x", 1200)
	total := maxUndoLines + 50
	for i := 0; i < total; i++ {
		e := UndoEntry{Op: "edited", TS: time.Now().UTC().Format(time.RFC3339Nano), Path: "/tmp/x.md", Before: padding}
		if err := RecordUndo(s, e); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(undoFile(s))
	if err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) > maxUndoLines {
		t.Errorf("journal has %d lines after compaction, want at most %d", len(lines), maxUndoLines)
	}
	if len(lines) == 0 {
		t.Fatal("journal should not be empty after compaction")
	}
}
