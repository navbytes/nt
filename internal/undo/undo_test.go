package undo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/navbytes/nt/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s := &store.Store{Dir: dir}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPeekEmptyJournal(t *testing.T) {
	s := newTestStore(t)
	_, ok, err := Peek(s)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Peek on empty/missing journal should report ok=false")
	}
}

func TestAppendPeekRoundTrip(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		if err := Append(s, Txn{Op: fmt.Sprintf("op%d", i), TS: "2026-07-08T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	txn, ok, err := Peek(s)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a pending transaction")
	}
	if txn.Op != "op4" {
		t.Errorf("Peek returned op %q, want the LAST appended (op4)", txn.Op)
	}
}

func TestReplaceLastSwapsOnlyTheTop(t *testing.T) {
	s := newTestStore(t)
	if err := Append(s, Txn{Op: "first", TS: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(s, Txn{Op: "second", TS: "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceLast(s, Txn{Op: "redo:second", TS: "t3"}); err != nil {
		t.Fatal(err)
	}
	txn, ok, err := Peek(s)
	if err != nil || !ok {
		t.Fatalf("Peek after ReplaceLast: ok=%v err=%v", ok, err)
	}
	if txn.Op != "redo:second" {
		t.Errorf("Peek = %q, want redo:second", txn.Op)
	}

	// The entry below the top must be untouched.
	data, err := os.ReadFile(s.UndoFile())
	if err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != 2 {
		t.Fatalf("journal has %d lines, want 2", len(lines))
	}
}

// A journal that grows past journalCompactThreshold must compact to
// maxJournalLines on the next Append, keeping only the most recent entries —
// Peek must still return the true last entry afterward.
func TestAppendCompactsOversizedJournal(t *testing.T) {
	s := newTestStore(t)
	// Seed the journal directly (bypassing Append's own compaction check) with
	// more than maxJournalLines padded lines so it crosses journalCompactThreshold,
	// then assert compaction triggers on the NEXT Append.
	f, err := os.OpenFile(s.UndoFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	total := maxJournalLines + 50
	padding := make([]byte, 1200)
	for i := range padding {
		padding[i] = 'x'
	}
	for i := 0; i < total; i++ {
		line := fmt.Sprintf(`{"op":"seed%d","ts":"t","changes":null,"_pad":"%s"}`, i, string(padding))
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(s.UndoFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= journalCompactThreshold {
		t.Fatalf("test setup didn't exceed compaction threshold: %d bytes", info.Size())
	}

	// This Append should trigger compaction.
	if err := Append(s, Txn{Op: "final", TS: "tFinal"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.UndoFile())
	if err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != maxJournalLines {
		t.Errorf("journal has %d lines after compaction, want exactly %d", len(lines), maxJournalLines)
	}

	txn, ok, err := Peek(s)
	if err != nil || !ok {
		t.Fatalf("Peek after compaction: ok=%v err=%v", ok, err)
	}
	if txn.Op != "final" {
		t.Errorf("Peek after compaction = %q, want the most recent entry (final)", txn.Op)
	}
}

func TestLastLineHandlesMultiChunkFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Force the backward scan to cross an 8KiB chunk boundary.
	big := make([]byte, 20000)
	for i := range big {
		big[i] = 'a'
	}
	if _, err := f.WriteString(fmt.Sprintf("first-%s\n", string(big[:100]))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(fmt.Sprintf("second-%s\n", string(big))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	line, ok, err := lastLine(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := fmt.Sprintf("second-%s", string(big))
	if line != want {
		t.Errorf("lastLine returned %d bytes, want %d matching the second line", len(line), len(want))
	}
}
