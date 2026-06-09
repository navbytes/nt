//go:build windows

package cli

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// detachAttr detaches the background server from this console and gives it its
// own process group, so it keeps running after the launching shell closes.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}

// signalStop terminates the server. Windows has no SIGTERM; the store's writes
// are atomic + flock-guarded, so a hard stop can't leave it half-written.
func signalStop(p *os.Process) error {
	return p.Kill()
}

// processAlive reports whether pid is a live process by checking its exit code
// (STILL_ACTIVE) — os.FindProcess always "succeeds" on Windows, so it can't be
// used as a liveness test.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
