// Package undo implements the append-only transaction journal from SPEC §6.3.
// Each forward mutation records one transaction capturing the before-images of
// every task line it touched (added, changed, or removed), keyed by ULID. Undo
// pops the last transaction and restores those before-images. The journal is
// written under the tasks.txt lock, in the same critical section as the
// mutation, so "last" is well-defined across processes.
package undo

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/navbytes/nt/internal/store"
)

// maxJournalLines bounds the journal to the most recent N transactions. Undo
// and redo are single-level — they only ever Peek/ReplaceLast the JOURNAL'S
// LAST LINE, alternating it between a forward op and its "redo:"-prefixed
// inverse (see mutate.applyReversal) — so entries below the top exist only as
// an audit trail, never as functional undo/redo state. Capping is therefore
// safe: it bounds a long-lived store's journal size with no behavior change.
const maxJournalLines = 500

// journalCompactThreshold is the file-size trigger for compaction, checked via
// a cheap os.Stat on every Append so the common case stays O(1); only once the
// journal crosses this size does Append pay the O(n) cost of rewriting it.
const journalCompactThreshold = 512 * 1024

// Change records one task's before/after raw lines. An empty Before means the
// task was newly added (undo deletes it); an empty After means it was removed
// (undo re-adds it).
type Change struct {
	ID     string `json:"id"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Txn is a single undoable transaction.
type Txn struct {
	Op      string   `json:"op"`           // human label, e.g. "add", "done", "archive"
	TS      string   `json:"ts"`           // RFC3339 timestamp
	WS      string   `json:"ws,omitempty"` // workstream that made the change ("" = unscoped/human)
	Changes []Change `json:"changes"`      // ULID-keyed before/after lines
}

// Append writes a transaction to the journal, then compacts it to the most
// recent maxJournalLines entries once its size crosses journalCompactThreshold
// (a stat on every call, an O(n) rewrite only occasionally). Caller holds the
// lock.
func Append(s *store.Store, t Txn) error {
	f, err := os.OpenFile(s.UndoFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	enc, err := json.Marshal(t)
	if err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	if _, err := f.Write(append(enc, '\n')); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	info, statErr := f.Stat()
	if err := f.Close(); err != nil {
		return err
	}
	if statErr != nil {
		return statErr
	}
	if info.Size() <= journalCompactThreshold {
		return nil
	}
	return compact(s)
}

// compact truncates the journal to its most recent maxJournalLines entries.
// Only ever called once Append detects the file has crossed
// journalCompactThreshold.
func compact(s *store.Store) error {
	data, err := store.ReadFile(s.UndoFile())
	if err != nil {
		return err
	}
	lines := nonEmptyLines(string(data))
	if len(lines) <= maxJournalLines {
		return nil
	}
	lines = lines[len(lines)-maxJournalLines:]
	out := strings.Join(lines, "\n") + "\n"
	return store.WriteAtomic(s.UndoFile(), []byte(out), 0o644)
}

// Peek returns the most recent transaction without modifying the journal, or
// ok=false if the journal is empty. Caller holds the lock. It is non-destructive
// by design: undo must apply (and durably write) the reverted tasks file BEFORE
// the journal entry is removed, so a write failure can't lose the inverse
// (SPEC §6.3 — "a crash can't leave a mutation without its inverse"). Pair it
// with ReplaceLast once the tasks write has succeeded.
func Peek(s *store.Store) (Txn, bool, error) {
	line, ok, err := lastLine(s.UndoFile())
	if err != nil || !ok {
		return Txn{}, false, err
	}
	var t Txn
	if err := json.Unmarshal([]byte(line), &t); err != nil {
		return Txn{}, false, err
	}
	return t, true, nil
}

// lastLine returns the last non-empty line of the file at path, reading
// backward from the end in chunks rather than loading and splitting the whole
// file — the journal only ever needs its tail (see maxJournalLines). ok is
// false if the file is missing or empty.
func lastLine(path string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	size := info.Size()
	if size == 0 {
		return "", false, nil
	}

	const chunk = 8192
	var buf []byte
	pos := size
	for pos > 0 {
		readSize := int64(chunk)
		if readSize > pos {
			readSize = pos
		}
		pos -= readSize
		tmp := make([]byte, readSize)
		if _, err := f.ReadAt(tmp, pos); err != nil {
			return "", false, err
		}
		buf = append(tmp, buf...)
		trimmed := strings.TrimRight(string(buf), "\n")
		if idx := strings.LastIndexByte(trimmed, '\n'); idx >= 0 {
			return trimmed[idx+1:], true, nil
		}
		if pos == 0 {
			if trimmed == "" {
				return "", false, nil
			}
			return trimmed, true, nil
		}
	}
	return "", false, nil
}

// ReplaceLast atomically removes the most recent transaction and appends next
// (the redo entry) in a single atomic write. Caller holds the lock. Called by
// undo AFTER the reverted tasks file is durably written, so the journal only
// drops the entry once the revert is committed.
func ReplaceLast(s *store.Store, next Txn) error {
	data, err := store.ReadFile(s.UndoFile())
	if err != nil {
		return err
	}
	lines := nonEmptyLines(string(data))
	if len(lines) > 0 {
		lines = lines[:len(lines)-1] // drop the popped transaction
	}
	enc, err := json.Marshal(next)
	if err != nil {
		return err
	}
	lines = append(lines, string(enc))
	out := strings.Join(lines, "\n") + "\n"
	return store.WriteAtomic(s.UndoFile(), []byte(out), 0o644)
}

func nonEmptyLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			out = append(out, sc.Text())
		}
	}
	return out
}
