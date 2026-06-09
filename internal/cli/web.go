package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/navbytes/nt/internal/store"
)

// detachedFlag is the hidden flag the parent sets on the re-exec'd child so the
// child knows to record its PID file. It is intentionally undocumented.
const detachedFlag = "__detached"

const (
	webPidFile = "web.pid" // $NT_DIR/web.pid — JSON record of the detached server
	webLogFile = "web.log" // $NT_DIR/web.log — the detached server's stdout/stderr
)

// webProc is the recorded detached `nt web --detach` server, so --status and
// --stop can find and manage it across separate invocations.
type webProc struct {
	PID     int    `json:"pid"`
	URL     string `json:"url"`
	Edit    bool   `json:"edit"`
	Started string `json:"started"` // RFC3339
}

func webStorePath(name string) (string, error) {
	dir, err := store.ResolveDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func readWebProc() (*webProc, error) {
	path, err := webStorePath(webPidFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err // includes fs.ErrNotExist
	}
	var p webProc
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func writeWebProc(p *webProc) error {
	path, err := webStorePath(webPidFile)
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	return store.WriteAtomic(path, append(data, '\n'), 0o644)
}

func removeWebPid() {
	if path, err := webStorePath(webPidFile); err == nil {
		_ = os.Remove(path)
	}
}

// runningWebProc returns the recorded server only if its process is still alive,
// clearing a stale PID file (e.g. after a crash or reboot) otherwise.
func runningWebProc() *webProc {
	p, err := readWebProc()
	if err != nil || p == nil {
		return nil
	}
	if !processAlive(p.PID) {
		removeWebPid()
		return nil
	}
	return p
}

// webDetach re-execs nt as a background server detached from this terminal,
// redirecting its output to $NT_DIR/web.log. The child writes the PID file with
// its real URL once bound (see cmdWeb's onReady).
func webDetach(host string, port int, edit bool) int {
	if p := runningWebProc(); p != nil {
		fmt.Printf("nt web is already running at %s (pid %d) — `nt web --stop` first\n", p.URL, p.PID)
		return 1
	}
	logPath, err := webStorePath(webLogFile)
	if err != nil {
		return fail(err)
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fail(fmt.Errorf("open %s: %w", logPath, err))
	}
	defer logf.Close()

	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	args := []string{"web", "--host", host, "--port", strconv.Itoa(port), "--" + detachedFlag}
	if edit {
		args = append(args, "--edit")
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, logf, logf
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("start detached server: %w", err))
	}
	// Don't Wait — the child is detached and owns its lifecycle now.
	fmt.Printf("nt web started in the background (pid %d), listening near http://%s:%d\n", cmd.Process.Pid, host, port)
	fmt.Printf("  logs:   %s\n", logPath)
	fmt.Printf("  manage: nt web --status   ·   nt web --stop\n")
	return 0
}

// webStop stops a backgrounded server and clears its PID file.
func webStop() int {
	p, err := readWebProc()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("nt web is not running (no background server recorded)")
			return 0
		}
		return fail(err)
	}
	if !processAlive(p.PID) {
		removeWebPid()
		fmt.Println("nt web is not running (cleared a stale PID file)")
		return 0
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return fail(err)
	}
	if err := signalStop(proc); err != nil {
		return fail(fmt.Errorf("stop pid %d: %w", p.PID, err))
	}
	removeWebPid()
	fmt.Printf("stopped nt web (pid %d)\n", p.PID)
	return 0
}

// webStatus reports whether a backgrounded server is running. It exits non-zero
// when none is, so `nt web --status && …` works in scripts.
func webStatus() int {
	p := runningWebProc()
	if p == nil {
		fmt.Println("nt web: not running")
		return 1
	}
	mode := "read-only"
	if p.Edit {
		mode = "editing"
	}
	fmt.Printf("nt web: running (pid %d) at %s [%s]\n", p.PID, p.URL, mode)
	return 0
}
