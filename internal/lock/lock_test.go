package lock

import (
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireContention is platform-agnostic: it drives whichever primitive the
// build selected (flock or LockFileEx) and asserts the shared contract — an
// exclusive lock excludes a second acquirer until released, and the wait is
// bounded by the timeout rather than blocking forever.
func TestAcquireContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.lock")

	h1, err := Acquire(path, DefaultTimeout)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// A second acquire must fail while the first is held, after ~the timeout.
	start := time.Now()
	if h2, err := Acquire(path, 100*time.Millisecond); err == nil {
		h2.Release()
		t.Fatal("second acquire should report busy while the lock is held")
	}
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Errorf("acquire returned too early (%s); should poll until the deadline", waited)
	}

	// After release the lock is free again.
	if err := h1.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	h3, err := Acquire(path, DefaultTimeout)
	if err != nil {
		t.Fatalf("acquire after release should succeed: %v", err)
	}
	if err := h3.Release(); err != nil {
		t.Fatalf("final release: %v", err)
	}
}

// TestReleaseNilSafe documents that Release tolerates a nil handle and a double
// release, so callers can `defer h.Release()` unconditionally.
func TestReleaseNilSafe(t *testing.T) {
	var h *Handle
	if err := h.Release(); err != nil {
		t.Errorf("nil-handle release: %v", err)
	}

	h2, err := Acquire(filepath.Join(t.TempDir(), "store.lock"), DefaultTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.Release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := h2.Release(); err != nil {
		t.Errorf("double release should be a no-op: %v", err)
	}
}
