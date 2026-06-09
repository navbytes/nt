package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ---- live reload (SSE + fsnotify) -----------------------------------------

type hub struct {
	mu      sync.Mutex
	clients map[chan string]bool
}

func newHub() *hub { return &hub{clients: map[chan string]bool{}} }

func (h *hub) add() chan string {
	ch := make(chan string, 4)
	h.mu.Lock()
	h.clients[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *hub) remove(ch chan string) {
	h.mu.Lock()
	if h.clients[ch] {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// broadcast pushes a typed event payload to every connected client. "reload"
// means "the page is stale, reload it" (an external change); finer kinds like
// "tasks" let a listener refresh just one fragment instead of the whole page.
func (h *hub) broadcast(kind string) {
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- kind:
		default:
		}
	}
	h.mu.Unlock()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.add()
	defer s.hub.remove(ch)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	// Heartbeat: a periodic comment keeps the connection healthy and lets the
	// server notice (and reap) a client that vanished without a clean close, so
	// goroutines/sockets don't linger.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case kind, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", kind)
			fl.Flush()
		}
	}
}

// watchDebounce coalesces a burst of fsnotify events (an atomic rename fires
// several) into one rebuild (SPEC §6.5).
const watchDebounce = 80 * time.Millisecond

// watch maintains the read-model from disk and broadcasts live-reload. It is
// debounced and self-write-aware: events for files the adapter just wrote (and
// for hidden/temp files like the .nt-*.tmp atomic-write staging file) don't
// bounce clients, and a burst collapses into a single rebuild. Idempotent —
// safe to call more than once (Serve and embedders both may).
func (s *Server) watch() {
	s.mu.Lock()
	if s.watching {
		s.mu.Unlock()
		return
	}
	s.watching = true
	s.mu.Unlock()

	wt, err := fsnotify.NewWatcher()
	if err != nil {
		s.mu.Lock()
		s.watching = false
		s.mu.Unlock()
		return
	}
	addDirs(wt, s.eng.S.NotesDir())
	_ = wt.Add(s.eng.S.Dir)
	s.rebuild() // ensure the maintained snapshot is current before serving from it

	go func() {
		defer func() { _ = wt.Close() }()
		var (
			bmu      sync.Mutex
			timer    *time.Timer
			external bool // batch contains a change we didn't make ourselves
		)
		flush := func() {
			bmu.Lock()
			ext := external
			external = false
			bmu.Unlock()
			s.rebuild()
			if ext {
				s.hub.broadcast("reload") // only nudge clients for changes they didn't trigger
			}
		}
		for {
			select {
			case ev, ok := <-wt.Events:
				if !ok {
					return
				}
				if isTransient(ev.Name) {
					continue // .nt-*.tmp staging, lock/undo/log — not content the UI shows
				}
				if ev.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = wt.Add(ev.Name) // watch newly-created subfolders
					}
				}
				bmu.Lock()
				if !s.writes.isSelf(ev.Name) {
					external = true
				}
				if timer == nil {
					timer = time.AfterFunc(watchDebounce, flush)
				} else {
					timer.Reset(watchDebounce)
				}
				bmu.Unlock()
			case _, ok := <-wt.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

// isTransient reports whether a path is store bookkeeping the viewer never
// renders — the atomic-write staging file, the lock, the undo journal, the log,
// and dotfiles (.git/.obsidian). Changes to these must not trigger a rebuild or
// a live-reload; only tasks.txt / done.txt / notes matter.
func isTransient(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return true
	}
	switch base {
	case "tasks.txt.lock", "undo.jsonl", "nt.log":
		return true
	}
	return false
}

func addDirs(wt *fsnotify.Watcher, root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			_ = wt.Add(p)
		}
		return nil
	})
}
