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

func TestDepCycleNotHidden(t *testing.T) {
	// A blocks B and B blocks A → a 2-cycle. Neither can ever unblock, so neither
	// should be hidden (that would be an invisible deadlock).
	doc := Parse([]byte("task a blocks:01B id:01A\ntask b blocks:01A id:01B\n"))
	tasks := doc.Tasks()

	cycles := DepCycles(tasks)
	if len(cycles) != 1 || len(cycles[0]) != 2 {
		t.Fatalf("expected one 2-cycle, got %v", cycles)
	}
	blocked := BlockedIDs(tasks)
	if blocked["01A"] || blocked["01B"] {
		t.Errorf("cycle members must stay visible (not blocked): %v", blocked)
	}
}

func TestDepCycleThreeNode(t *testing.T) {
	doc := Parse([]byte("a blocks:01B id:01A\nb blocks:01C id:01B\nc blocks:01A id:01C\n"))
	cycles := DepCycles(doc.Tasks())
	if len(cycles) != 1 || len(cycles[0]) != 3 {
		t.Fatalf("expected one 3-cycle, got %v", cycles)
	}
}

func TestNoCycleForChain(t *testing.T) {
	// A → B → C is a valid chain, not a cycle.
	doc := Parse([]byte("a blocks:01B id:01A\nb blocks:01C id:01B\nc id:01C\n"))
	if cycles := DepCycles(doc.Tasks()); len(cycles) != 0 {
		t.Errorf("a chain is not a cycle, got %v", cycles)
	}
}

func TestDanglingBlocks(t *testing.T) {
	doc := Parse([]byte("orphan blocks:01GONE id:01A\nfine task id:01B\n"))
	d := DanglingBlocks(doc.Tasks())
	if len(d) != 1 || d[0] != "01A" {
		t.Errorf("expected task 01A flagged for a dangling blocks:, got %v", d)
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
