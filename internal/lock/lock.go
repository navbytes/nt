// Package lock provides an advisory file lock around the critical section that
// mutates tasks.txt (SPEC §6.4). It locks a dedicated lock file — never
// tasks.txt itself — so the atomic rename that replaces tasks.txt can't swap
// the inode out from under a held lock.
//
// flock(2) is local-filesystem only; on NFS it may silently no-op. The store is
// documented as local-FS only for concurrent access.
package lock

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// DefaultTimeout bounds how long Acquire waits before reporting the store busy.
const DefaultTimeout = 2 * time.Second

// Handle is a held lock. Release must be called (typically via defer).
type Handle struct {
	f *os.File
}

// Acquire takes an exclusive advisory lock on path, waiting up to timeout. It
// returns a clear "store is busy" error rather than blocking forever or
// silently succeeding.
func Acquire(path string, timeout time.Duration) (*Handle, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Handle{f: f}, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, fmt.Errorf("lock: %w", err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("nt store is busy (lock held > %s); another nt process may be writing", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Release unlocks and closes the lock file.
func (h *Handle) Release() error {
	if h == nil || h.f == nil {
		return nil
	}
	_ = syscall.Flock(int(h.f.Fd()), syscall.LOCK_UN)
	err := h.f.Close()
	h.f = nil
	return err
}
