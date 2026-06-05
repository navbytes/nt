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
