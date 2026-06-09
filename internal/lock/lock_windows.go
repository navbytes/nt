//go:build windows

package lock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// On Windows there is no flock; LockFileEx provides the equivalent advisory,
// process-scoped, auto-released byte-range lock. We lock the maximal range from
// offset 0 (low=high=0xFFFFFFFF) so the empty lock file is covered regardless of
// size — the standard whole-file idiom. LOCKFILE_FAIL_IMMEDIATELY makes the call
// non-blocking so Acquire's poll loop owns the timeout, matching the Unix path.
const lockRangeLow, lockRangeHigh = 0xFFFFFFFF, 0xFFFFFFFF

// tryLockExclusive attempts a non-blocking exclusive lock. It reports ok=false
// (with a nil error) when another process holds the lock (ERROR_LOCK_VIOLATION),
// so the caller retries until its deadline rather than treating it as fatal.
func tryLockExclusive(f *os.File) (ok bool, err error) {
	var ol windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, lockRangeLow, lockRangeHigh, &ol,
	)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

// unlock releases the byte-range lock. It must name the same range that was
// locked. The lock is also dropped when the handle closes / the process exits.
func unlock(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockRangeLow, lockRangeHigh, &ol)
}
