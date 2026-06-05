package task

import "testing"

func TestBlockedIDs(t *testing.T) {
	doc := Parse([]byte("deploy api id:01TARGET\nwrite migration blocks:01TARGET id:01BLOCKER\n"))
	tasks := doc.Tasks()
	blocked := BlockedIDs(tasks)
	if !blocked["01TARGET"] {
		t.Error("01TARGET should be blocked while its blocker is open")
	}
	if blocked["01BLOCKER"] {
		t.Error("the blocker itself should not be blocked")
	}

	// Completing the blocker unblocks the target.
	tasks[1].SetDone(true, "2026-06-06")
	if BlockedIDs(tasks)["01TARGET"] {
		t.Error("01TARGET should unblock once the blocker is done")
	}
}

func TestEffectiveStatus(t *testing.T) {
	open := Parse([]byte("plain task id:01A\n")).Tasks()[0]
	if got := EffectiveStatus(open, false); got != "open" {
		t.Errorf("open: got %q", got)
	}
	if got := EffectiveStatus(open, true); got != "blocked" {
		t.Errorf("blocked-by-dep: got %q", got)
	}
	done := Parse([]byte("x done thing id:01B\n")).Tasks()[0]
	if got := EffectiveStatus(done, true); got != "done" {
		t.Errorf("done overrides blocked: got %q", got)
	}
}
