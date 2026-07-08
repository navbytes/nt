package note

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/store"
)

// UndoEntry is one undoable note-body edit: the file's exact raw bytes
// (frontmatter + body) before the edit. Unlike the task journal's Change
// (which records both before AND after per task line, because a task Doc is
// validated line-by-line against a post-image), a note edit only ever needs
// its before-image — a note is a single file, so the after-image is always
// just "whatever's on disk now," recoverable by reading it.
type UndoEntry struct {
	Op     string `json:"op"`           // human label: "appended to" | "replaced body of" | "edited" | "set description of"
	TS     string `json:"ts"`           // RFC3339Nano
	WS     string `json:"ws,omitempty"` // workstream that made the edit ("" = unscoped)
	Path   string `json:"path"`
	Before string `json:"before"` // the file's full raw bytes before the edit
}

// undoFile is a sibling of the task journal (undo.jsonl), not the same file:
// retrofitting notes into the task journal's Txn/Change shape (and its Doc
// post-image validation) would be substantial, risky surgery on well-tested
// task-undo logic for very little shared benefit — notes and tasks revert
// completely differently (whole-file restore vs. per-line inverse).
func undoFile(s *store.Store) string { return filepath.Join(s.Dir, "notes-undo.jsonl") }

// maxUndoLines and compactThreshold mirror internal/undo's task-journal
// discipline (see that package's maxJournalLines/journalCompactThreshold):
// only the last entry is ever read or replaced (single-level undo/redo, same
// as tasks), so capping is behavior-safe and bounds a long-lived,
// frequently-edited store's journal size.
const (
	maxUndoLines     = 500
	compactThreshold = 512 * 1024
)

// RecordUndo appends an undo entry for a note edit, then compacts the journal
// once it grows past compactThreshold. Call it with the note's raw file bytes
// captured BEFORE the edit was written.
func RecordUndo(s *store.Store, e UndoEntry) error {
	f, err := os.OpenFile(undoFile(s), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	enc, err := json.Marshal(e)
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
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	if statErr != nil {
		return statErr
	}
	if info.Size() <= compactThreshold {
		return nil
	}
	return compactUndo(s)
}

func compactUndo(s *store.Store) error {
	data, err := store.ReadFile(undoFile(s))
	if err != nil {
		return err
	}
	lines := nonEmptyLines(string(data))
	if len(lines) <= maxUndoLines {
		return nil
	}
	lines = lines[len(lines)-maxUndoLines:]
	out := strings.Join(lines, "\n") + "\n"
	return store.WriteAtomic(undoFile(s), []byte(out), 0o644)
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

// PeekUndo returns the most recent pending note-undo entry, or ok=false if
// there is none.
func PeekUndo(s *store.Store) (UndoEntry, bool, error) {
	data, err := store.ReadFile(undoFile(s))
	if err != nil {
		if os.IsNotExist(err) {
			return UndoEntry{}, false, nil
		}
		return UndoEntry{}, false, err
	}
	lines := nonEmptyLines(string(data))
	if len(lines) == 0 {
		return UndoEntry{}, false, nil
	}
	var e UndoEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &e); err != nil {
		return UndoEntry{}, false, err
	}
	return e, true, nil
}

// popAndSwap removes the top entry and pushes next in its place — the same
// undo/redo toggle the task journal uses (internal/undo.ReplaceLast).
func popAndSwap(s *store.Store, next UndoEntry) error {
	data, err := store.ReadFile(undoFile(s))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := nonEmptyLines(string(data))
	if len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	enc, err := json.Marshal(next)
	if err != nil {
		return err
	}
	lines = append(lines, string(enc))
	out := strings.Join(lines, "\n") + "\n"
	return store.WriteAtomic(undoFile(s), []byte(out), 0o644)
}

// IsRedoEntry mirrors the task journal's convention (mutate.isRedoEntry): an
// odd number of "redo:" prefixes on Op means the entry re-applies a
// previously undone edit rather than being a fresh forward change.
func IsRedoEntry(op string) bool {
	n := 0
	for strings.HasPrefix(op, "redo:") {
		op = op[len("redo:"):]
		n++
	}
	return n%2 == 1
}

// DisplayOp strips the internal "redo:" prefixes off an op label for messages.
func DisplayOp(op string) string {
	for strings.HasPrefix(op, "redo:") {
		op = op[len("redo:"):]
	}
	return op
}

// ApplyReversal reverts (or re-applies, if wantRedo) the most recent pending
// note edit: it restores the file's exact prior bytes and swaps the journal
// entry to a "redo:"-prefixed inverse holding the bytes it just overwrote —
// the same toggle-in-place mechanic mutate.applyReversal uses for tasks.
// did is false when there's nothing pending, or the pending entry doesn't
// match wantRedo (a plain undo won't re-apply a redo entry, and vice versa).
// ws/force mirror the task journal's shared-store ownership check: on a
// shared store, reverting another workstream's note edit is refused unless
// force is set.
func ApplyReversal(s *store.Store, ws string, force, wantRedo bool) (path, op string, did bool, err error) {
	e, ok, perr := PeekUndo(s)
	if perr != nil || !ok {
		return "", "", false, perr
	}
	if IsRedoEntry(e.Op) != wantRedo {
		return "", "", false, nil
	}
	if !force && ws != "" && e.WS != ws {
		owner := "an unscoped writer (no workstream)"
		if e.WS != "" {
			owner = fmt.Sprintf("workstream %q", e.WS)
		}
		verb := "undo"
		if wantRedo {
			verb = "redo"
		}
		return "", "", false, fmt.Errorf("the last note change (%s, %s) was made by %s, not yours (%q) — on a shared store reverting another agent's edit loses data; rerun `nt %s --force` if you really mean it", DisplayOp(e.Op), e.TS, owner, ws, verb)
	}

	current, rerr := os.ReadFile(e.Path)
	if rerr != nil {
		return "", "", false, fmt.Errorf("undo: %s is missing on disk — can't revert (it may have been moved or deleted since)", e.Path)
	}
	if err := store.WriteAtomic(e.Path, []byte(e.Before), 0o644); err != nil {
		return "", "", false, err
	}
	next := UndoEntry{Op: "redo:" + e.Op, TS: time.Now().UTC().Format(time.RFC3339Nano), WS: e.WS, Path: e.Path, Before: string(current)}
	if err := popAndSwap(s, next); err != nil {
		return "", "", false, err
	}
	return e.Path, DisplayOp(e.Op), true, nil
}

// PendingTS returns the pending note-undo entry's timestamp (RFC3339Nano) and
// whether one exists, without changing anything — used to decide whether `nt
// undo` should target the note journal or the task journal (whichever is
// more recent). Parse failures report ok=false so a corrupt/old-format
// timestamp never wins the race by accident.
func PendingTS(s *store.Store) (ts time.Time, ok bool) {
	e, found, err := PeekUndo(s)
	if err != nil || !found {
		return time.Time{}, false
	}
	t, perr := time.Parse(time.RFC3339Nano, e.TS)
	if perr != nil {
		return time.Time{}, false
	}
	return t, true
}
