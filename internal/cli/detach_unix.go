//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// detachAttr puts the background server in its own session (setsid) so it
// outlives the terminal that launched it and ignores its controlling TTY's
// SIGHUP on close.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// signalStop asks the server to shut down gracefully (SIGTERM).
func signalStop(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// processAlive reports whether pid is a live process. Signal 0 performs the
// permission/existence check without delivering a signal.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid) // never fails on Unix; the signal is the real test
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
