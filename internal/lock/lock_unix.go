//go:build !windows

package lock

import (
	"os"
	"syscall"
)

// tryLockExclusive attempts a non-blocking exclusive flock. It reports ok=false
// (with a nil error) when another process currently holds the lock — the caller
// treats that as "retry until the deadline", distinct from a hard error.
func tryLockExclusive(f *os.File) (ok bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch err {
	case nil:
		return true, nil
	case syscall.EWOULDBLOCK:
		return false, nil
	default:
		return false, err
	}
}

// unlock releases the flock. The lock is also dropped when the fd closes.
func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
