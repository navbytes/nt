package task

import "testing"

func TestAdvance(t *testing.T) {
	cases := []struct{ date, rec, want string }{
		{"2026-06-06", "weekly", "2026-06-13"},
		{"2026-06-06", "daily", "2026-06-07"},
		{"2026-06-06", "3d", "2026-06-09"},
		{"2026-06-06", "2w", "2026-06-20"},
		{"2026-06-06", "monthly", "2026-07-06"},
		{"2026-06-06", "P3D", "2026-06-09"},
		{"2026-06-06", "1y", "2027-06-06"},
		{"2026-06-06", "bogus", "2026-06-06"}, // unparseable → unchanged
	}
	for _, c := range cases {
		if got := advance(c.date, c.rec); got != c.want {
			t.Errorf("advance(%q,%q)=%q want %q", c.date, c.rec, got, c.want)
		}
	}
}

func TestMonthEndClamp(t *testing.T) {
	// Jan 31 + 1 month must clamp to Feb 28 (2026 isn't a leap year), not
	// overflow into March (Go's AddDate would normalize to Mar 3).
	if got := advance("2026-01-31", "monthly"); got != "2026-02-28" {
		t.Errorf("Jan 31 + 1mo = %q, want 2026-02-28", got)
	}
	if got := advance("2028-01-31", "1m"); got != "2028-02-29" { // leap year
		t.Errorf("Jan 31 2028 + 1mo = %q, want 2028-02-29", got)
	}
}

func TestNextDueStrictRollsForward(t *testing.T) {
	// A strict monthly task due on the 1st, completed two months late, rolls
	// forward to the next future 1st — never an overdue occurrence.
	tk := Parse([]byte("pay rent due:2026-01-01 rec:+monthly id:01ABC\n")).Tasks()[0]
	got := NextDue(tk, "2026-03-15")
	if got != "2026-04-01" {
		t.Errorf("strict roll-forward: got %q want 2026-04-01", got)
	}
	if got < "2026-03-15" {
		t.Errorf("strict recurrence must never schedule before today, got %q", got)
	}
}

func TestNextDueFloatingFromCompletion(t *testing.T) {
	// A plain (floating) recurrence schedules from the completion date.
	tk := Parse([]byte("water plant due:2026-01-01 rec:3d id:01ABC\n")).Tasks()[0]
	if got := NextDue(tk, "2026-06-10"); got != "2026-06-13" {
		t.Errorf("floating from completion: got %q want 2026-06-13", got)
	}
}

func TestAdvanceDueSkip(t *testing.T) {
	tk := Parse([]byte("standup due:2026-06-06 rec:weekly id:01ABC\n")).Tasks()[0]
	if got := AdvanceDue(tk, "2026-06-06"); got != "2026-06-13" {
		t.Errorf("skip one week: got %q want 2026-06-13", got)
	}
	plain := Parse([]byte("one-off due:2026-06-06 id:01XYZ\n")).Tasks()[0]
	if got := AdvanceDue(plain, "2026-06-06"); got != "" {
		t.Errorf("non-recurring skip should be empty, got %q", got)
	}
}

func TestSpawnNextCarriesParent(t *testing.T) {
	tk := Parse([]byte("weekly subtask rec:+weekly parent:01PARENT due:2026-06-06 id:01ABC\n")).Tasks()[0]
	n := SpawnNext(tk, "2026-06-06")
	if n == nil || n.Parent() != "01PARENT" {
		t.Fatalf("spawned occurrence should keep its parent, got %v", n)
	}
}

func TestSpawnNextUnparseableIsNil(t *testing.T) {
	tk := Parse([]byte("bad rec:bogus id:01ABC\n")).Tasks()[0]
	if n := SpawnNext(tk, "2026-06-06"); n != nil {
		t.Errorf("unparseable recurrence should spawn nothing, got %v", n)
	}
}

func TestSpawnNext(t *testing.T) {
	tk := Parse([]byte("(A) weekly review @ops due:2026-06-06 rec:weekly src:cli id:01ABC\n")).Tasks()[0]
	n := SpawnNext(tk, "2026-06-06")
	if n.Due() != "2026-06-13" {
		t.Errorf("spawn due: got %q want 2026-06-13", n.Due())
	}
	if n.Recur() != "weekly" {
		t.Errorf("spawn rec: got %q", n.Recur())
	}
	if n.Priority != 'A' {
		t.Errorf("spawn priority: got %q want A", n.Priority)
	}
	if n.Source() != "cli" {
		t.Errorf("spawn src: got %q", n.Source())
	}
	if n.ID() == "" || n.ID() == "01ABC" {
		t.Errorf("spawn should get a fresh ULID, got %q", n.ID())
	}
	if n.Done {
		t.Error("spawn should be open")
	}
}
