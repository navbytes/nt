package tui

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// changedMsg signals that the store changed on disk (another process wrote it).
type changedMsg struct{}

// watchStore watches the store DIRECTORY (not the files) so our own atomic
// rename — and any editor's save-via-rename — doesn't invalidate the watch
// (SPEC §6.5). Events are debounced ~80ms and coalesced onto a single channel.
func watchStore(dir string) (<-chan struct{}, func(), error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}
	_ = w.Add(dir)
	_ = w.Add(filepath.Join(dir, "notes"))

	out := make(chan struct{}, 1)
	go func() {
		var timer *time.Timer
		fire := func() {
			select {
			case out <- struct{}{}:
			default:
			}
		}
		for {
			select {
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(80*time.Millisecond, fire)
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out, func() { _ = w.Close() }, nil
}

// waitForChange blocks until the next coalesced store change, then yields a
// changedMsg. Update re-issues it to keep listening.
func waitForChange(ch <-chan struct{}) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return changedMsg{}
	}
}
