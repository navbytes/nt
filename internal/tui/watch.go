package tui

import (
	"os"
	"path/filepath"
	"strings"
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
	addNoteDirs(w, filepath.Join(dir, "notes")) // recursive: foldered notes refresh too

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
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				// A new note subfolder must itself be watched so its files
				// trigger refreshes (fsnotify isn't recursive).
				if ev.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = w.Add(ev.Name)
					}
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

// addNoteDirs recursively watches the notes tree so changes in subfolders (which
// renameNote can create) trigger live refreshes, not just the top-level dir.
func addNoteDirs(w *fsnotify.Watcher, root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			_ = w.Add(p)
		}
		return nil
	})
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
