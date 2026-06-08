package web

import (
	"os"
	"testing"
)

// TestSnapshotSurfacesReadError: a store that can't be read must surface a
// readErr (shown as a UI warning) rather than rendering a silent empty store.
func TestSnapshotSurfacesReadError(t *testing.T) {
	s := newTestServer(t)
	// Make tasks.txt unreadable by replacing it with a directory → eng.Read errors.
	tf := s.eng.S.TasksFile()
	_ = os.Remove(tf) // may not exist yet in a fresh store
	if err := os.Mkdir(tf, 0o755); err != nil {
		t.Fatal(err)
	}
	if snap := buildSnapshot(s.eng, s.notes); snap.readErr == "" {
		t.Fatal("expected buildSnapshot to surface a read error")
	}
}
