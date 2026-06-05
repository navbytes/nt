// Package aisync mirrors a Claude Code session's TodoWrite list into the nt
// store (SPEC §8, §10). It is driven by a PostToolUse hook: Claude rewrites its
// whole todo list on every change, so the sync is idempotent — it keeps a
// per-session map of todo → nt ULID and upserts rather than duplicating.
package aisync

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/task"
)

// payload is the subset of a Claude Code PostToolUse hook event we care about.
type payload struct {
	ToolName  string `json:"tool_name"`
	SessionID string `json:"session_id"`
	ToolInput struct {
		Todos []todo `json:"todos"`
	} `json:"tool_input"`
}

type todo struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed
}

// key identifies a todo stably across TodoWrite rewrites: by id when present,
// else by content.
func (t todo) key(session string) string {
	k := t.ID
	if k == "" {
		k = t.Content
	}
	return session + "\x00" + k
}

// Sync applies a hook payload to the store. Non-TodoWrite events and malformed
// input are ignored (returns nil) so a hook never breaks the session.
func Sync(eng *mutate.Engine, data []byte) error {
	var p payload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil
	}
	if p.ToolName != "TodoWrite" || len(p.ToolInput.Todos) == 0 {
		return nil
	}

	st := loadState(eng.S)
	fresh := map[string]string{}
	err := eng.Apply("claude-sync", func(d *task.Doc, rec *mutate.Recorder) error {
		for _, td := range p.ToolInput.Todos {
			if strings.TrimSpace(td.Content) == "" {
				continue
			}
			k := td.key(p.SessionID)
			if id, ok := st[k]; ok {
				if tk := d.FindByID(id); tk != nil {
					rec.Before(tk)
					applyStatus(tk, td.Status)
					continue
				}
			}
			tk := task.New(td.Content)
			tk.SetKey("src", "claude")
			applyStatus(tk, td.Status)
			d.Append(tk)
			rec.Added(tk)
			fresh[k] = tk.ID()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(fresh) == 0 {
		return nil
	}
	for k, v := range fresh {
		st[k] = v
	}
	return saveState(eng.S, st)
}

// applyStatus maps a TodoWrite status onto an nt task.
func applyStatus(tk *task.Task, status string) {
	switch status {
	case "completed":
		if !tk.Done {
			tk.SetDone(true, mutate.Today())
		}
	case "in_progress":
		if tk.Done {
			tk.SetDone(false, "")
		}
		tk.SetState("doing")
	default: // pending / unknown
		if tk.Done {
			tk.SetDone(false, "")
		}
		tk.SetState("open")
	}
}

type state map[string]string

func statePath(s *store.Store) string { return filepath.Join(s.Dir, ".claude-sync.json") }

func loadState(s *store.Store) state {
	data, err := store.ReadFile(statePath(s))
	if err != nil || len(data) == 0 {
		return state{}
	}
	var st state
	if json.Unmarshal(data, &st) != nil {
		return state{}
	}
	return st
}

func saveState(s *store.Store, st state) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return store.WriteAtomic(statePath(s), data, 0o644)
}
