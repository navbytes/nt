package task

import "testing"

// TestRoundTripByteIdentical enforces SPEC §4: parse→render of an unmodified
// document must be byte-identical, including blank lines, comments, unknown
// key:value tokens, odd spacing, and trailing-newline state.
func TestRoundTripByteIdentical(t *testing.T) {
	inputs := []string{
		"",
		"\n",
		"(A) fix auth bug +api @backend due:2026-06-05 [[jwt]] src:claude id:01JZ8RT3\n",
		"x 2026-06-05 2026-06-01 write migration +api parent:01JZ8RT3 id:01JZ8RT9\n",
		"# a header comment line\n\nplain task no metadata\n",
		"task with    weird   spacing  and t:2026-01-01 h:1 unknown:keep id:01ABC\n",
		"no trailing newline",
		"(B) call dentist @phone\nx done thing\n\n",
	}
	for _, in := range inputs {
		got := string(Parse([]byte(in)).Render())
		if got != in {
			t.Errorf("round-trip mismatch\n in: %q\nout: %q", in, got)
		}
	}
}

func TestParseFields(t *testing.T) {
	d := Parse([]byte("(A) fix auth bug +api @backend due:2026-06-05 [[jwt]] src:claude id:01JZ8RT3\n"))
	tasks := d.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	tk := tasks[0]
	if tk.Priority != 'A' {
		t.Errorf("priority: want A, got %q", tk.Priority)
	}
	if tk.Due() != "2026-06-05" {
		t.Errorf("due: got %q", tk.Due())
	}
	if tk.Source() != "claude" {
		t.Errorf("src: got %q", tk.Source())
	}
	if tk.ID() != "01JZ8RT3" {
		t.Errorf("id: got %q", tk.ID())
	}
	if got := tk.Projects(); len(got) != 1 || got[0] != "api" {
		t.Errorf("projects: got %v", got)
	}
	if got := tk.Tags(); len(got) != 1 || got[0] != "backend" {
		t.Errorf("tags: got %v", got)
	}
	if got := tk.Links(); len(got) != 1 || got[0] != "jwt" {
		t.Errorf("links: got %v", got)
	}
	if tk.Status() != "open" {
		t.Errorf("status: got %q", tk.Status())
	}
}

// TestColonWordNotMetadata guards the parser against treating a prose word that
// ends in a colon (empty value) as a key:value token.
func TestColonWordNotMetadata(t *testing.T) {
	tk := Parse([]byte("spike: rotate auth secrets id:01ABC\n")).Tasks()[0]
	if tk.Text != "spike: rotate auth secrets" {
		t.Errorf("text: got %q want %q", tk.Text, "spike: rotate auth secrets")
	}
	if v, ok := tk.get("spike"); ok {
		t.Errorf("'spike:' should not be a key, got value %q", v)
	}
}

func TestDonePreservesPriority(t *testing.T) {
	d := Parse([]byte("(A) ship it id:01ABC\n"))
	tk := d.Tasks()[0]
	tk.SetDone(true, "2026-06-06")
	line := tk.Line()
	want := "x 2026-06-06 ship it pri:A id:01ABC"
	if line != want {
		t.Errorf("done render:\n got %q\nwant %q", line, want)
	}
	tk.SetDone(false, "")
	if tk.Priority != 'A' {
		t.Errorf("reopen should restore priority A, got %q", tk.Priority)
	}
	if got := tk.Line(); got != "(A) ship it id:01ABC" {
		t.Errorf("reopen render: got %q", got)
	}
}
