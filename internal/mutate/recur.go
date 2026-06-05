package mutate

import "github.com/navbytes/nt/internal/task"

// Complete marks a task done, spawning the next occurrence if it recurs (SPEC
// §9). Both the completion and the spawned task are recorded in the same undo
// transaction (rec captures the original's before-image and the spawn as an
// add), so a single `nt undo` reopens the original and removes the new one
// (SPEC §6.3). Must run inside an Engine.Apply closure.
func Complete(d *task.Doc, rec *Recorder, t *task.Task, today string) {
	rec.Before(t)
	var spawn *task.Task
	if !t.Done && t.Recur() != "" {
		spawn = task.SpawnNext(t, today) // read fields while still open
	}
	t.SetDone(true, today)
	if spawn != nil {
		d.Append(spawn)
		rec.Added(spawn)
	}
}
