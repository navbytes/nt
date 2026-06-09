// Package lock provides an advisory file lock around the critical section that
// mutates tasks.txt (SPEC §6.4). It locks a dedicated lock file — never
// tasks.txt itself — so the atomic rename that replaces tasks.txt can't swap
// the inode out from under a held lock.
//
// The OS primitive differs per platform: flock(2) on Unix, LockFileEx on
// Windows (see lock_unix.go / lock_windows.go). Both are advisory and released
// automatically when the file handle closes or the process exits, so a crash
// can't strand the store. Both are local-filesystem only; on NFS/SMB the lock
// may silently no-op, so the store is documented as local-FS only for
// concurrent access.
package lock

import (
	"fmt"
	"os"
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
// silently succeeding. The OS lock primitive is non-blocking (tryLockExclusive)
// and we poll, so the timeout is honored on every platform.
func Acquire(path string, timeout time.Duration) (*Handle, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := tryLockExclusive(f)
		if ok {
			return &Handle{f: f}, nil
		}
		if err != nil {
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
	_ = unlock(h.f)
	err := h.f.Close()
	h.f = nil
	return err
}
