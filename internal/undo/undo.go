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
	Op      string   `json:"op"`      // human label, e.g. "add", "done", "archive"
	TS      string   `json:"ts"`      // RFC3339 timestamp
	Changes []Change `json:"changes"` // ULID-keyed before/after lines
}

// Append writes a transaction to the journal. Caller holds the lock.
func Append(s *store.Store, t Txn) error {
	f, err := os.OpenFile(s.UndoFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(enc, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// Pop removes and returns the most recent transaction, or ok=false if the
// journal is empty. Caller holds the lock. It rewrites the journal without the
// popped line (atomic write).
func Pop(s *store.Store) (Txn, bool, error) {
	data, err := store.ReadFile(s.UndoFile())
	if err != nil {
		return Txn{}, false, err
	}
	lines := nonEmptyLines(string(data))
	if len(lines) == 0 {
		return Txn{}, false, nil
	}
	last := lines[len(lines)-1]
	var t Txn
	if err := json.Unmarshal([]byte(last), &t); err != nil {
		return Txn{}, false, err
	}
	remaining := strings.Join(lines[:len(lines)-1], "\n")
	if remaining != "" {
		remaining += "\n"
	}
	if err := store.WriteAtomic(s.UndoFile(), []byte(remaining), 0o644); err != nil {
		return Txn{}, false, err
	}
	return t, true, nil
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
