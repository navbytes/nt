package quickadd

import (
	"testing"
	"time"
)

func nextWeekday(wd time.Weekday) string {
	now := time.Now()
	days := (int(wd) - int(now.Weekday()) + 7) % 7
	if days == 0 {
		days = 7
	}
	return now.AddDate(0, 0, days).Format("2006-01-02")
}

func TestNormalizesInlineDueWord(t *testing.T) {
	tk := New("pay rent due:fri")
	want := nextWeekday(time.Friday)
	if tk.Due() != want {
		t.Fatalf("due = %q, want %q", tk.Due(), want)
	}
	if tk.Text != "pay rent" {
		t.Fatalf("text = %q, want %q", tk.Text, "pay rent")
	}
}

func TestNormalizesStartDate(t *testing.T) {
	tk := New("prep talk t:tomorrow")
	want := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if tk.Start() != want {
		t.Fatalf("start = %q, want %q", tk.Start(), want)
	}
}

func TestISODueUnchanged(t *testing.T) {
	tk := New("file taxes due:2026-04-15")
	if tk.Due() != "2026-04-15" {
		t.Fatalf("due = %q, want 2026-04-15", tk.Due())
	}
}

func TestUnparseableDueLeftLiteral(t *testing.T) {
	tk := New("ship it due:someday")
	if tk.Due() != "someday" {
		t.Fatalf("unparseable due should be left as-is, got %q", tk.Due())
	}
}

func TestLiftsPriorityMarker(t *testing.T) {
	for _, tc := range []struct {
		text string
		want byte
	}{
		{"call bob !high", 'A'},
		{"review pr !a", 'A'},
		{"tidy desk !low", 'C'},
		{"plan sprint !b", 'B'},
	} {
		tk := New(tc.text)
		if tk.Priority != tc.want {
			t.Errorf("%q: priority = %q, want %q", tc.text, tk.Priority, tc.want)
		}
		if tk.Text == tc.text {
			t.Errorf("%q: marker not removed from text (%q)", tc.text, tk.Text)
		}
	}
}

func TestPriorityMarkerDoesNotOverrideExplicit(t *testing.T) {
	tk := New("(A) urgent thing !low")
	if tk.Priority != 'A' {
		t.Fatalf("explicit (A) priority should win, got %q", tk.Priority)
	}
}

func TestBareBangIsNotPriority(t *testing.T) {
	tk := New("fix bug! now")
	if tk.Priority != 0 {
		t.Fatalf("a literal '!' word should not set priority, got %q", tk.Priority)
	}
	if tk.Text != "fix bug! now" {
		t.Fatalf("text should be unchanged, got %q", tk.Text)
	}
}

func TestCombinedDueAndPriority(t *testing.T) {
	tk := New("ship report due:tomorrow !high @work")
	if tk.Due() != time.Now().AddDate(0, 0, 1).Format("2006-01-02") {
		t.Errorf("due not normalized: %q", tk.Due())
	}
	if tk.Priority != 'A' {
		t.Errorf("priority not lifted: %q", tk.Priority)
	}
	if tags := tk.Tags(); len(tags) != 1 || tags[0] != "work" {
		t.Errorf("@work context should survive: %v", tags)
	}
}

func TestNewAssignsID(t *testing.T) {
	tk := New("a fresh task")
	if tk.ID() == "" {
		t.Fatal("quickadd.New should assign an id")
	}
}

// A title containing "id:<value>" as plain prose must never hijack the task's
// identity: task.ParseLine treats id: as a real todo.txt field (it must, to
// round-trip a genuine stored line), but New parses UNTRUSTED freeform text,
// where an "id:" token is either an accident or an attempted collision, never
// a legitimate way to set the id — that always comes from EnsureID.
func TestStructuralKeysNotInjectableFromText(t *testing.T) {
	victim := New("existing task")
	attacker := New("malicious collide id:" + victim.ID())
	if attacker.ID() == victim.ID() {
		t.Fatalf("freeform text set id: and collided with an existing task: %q", attacker.ID())
	}
	if attacker.ID() == "" {
		t.Fatal("quickadd.New should still assign a fresh id despite the id: token in text")
	}
	// Same for the other structural keys: ws/parent/blocks/discovered are only
	// ever meant to be set afterward via validated flags/params, not sniffed
	// out of prose.
	for _, key := range []string{"ws", "parent", "blocks", "discovered"} {
		tk := New("a task " + key + ":something")
		if tk.Key(key) != "" {
			t.Errorf("%s: should not be settable from freeform text, got %q", key, tk.Key(key))
		}
	}
}
