package task

import "testing"

func TestVisibleInList(t *testing.T) {
	open := Parse([]byte("open task id:01A\n")).Tasks()[0]
	done := Parse([]byte("x done task id:01B\n")).Tasks()[0]

	cases := []struct {
		name                           string
		tk                             *Task
		blocked, inclDone, inclBlocked bool
		want                           bool
	}{
		{"open default", open, false, false, false, true},
		{"open blocked default hides", open, true, false, false, false},
		{"open blocked with showBlocked", open, true, false, true, true},
		{"done default hides", done, false, false, false, false},
		{"done with includeDone", done, false, true, false, true},
		{"done+blocked still shows with includeDone (done overrides blocked)", done, true, true, false, true},
	}
	for _, c := range cases {
		if got := VisibleInList(c.tk, c.blocked, c.inclDone, c.inclBlocked); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestDueBucketHandlesTimeOfDay(t *testing.T) {
	today, weekEnd := "2026-06-09", "2026-06-16"
	mk := func(line string) *Task { return Parse([]byte(line + "\n")).Tasks()[0] }

	cases := []struct {
		line, want string
	}{
		{"x done id:01A", BucketDone},
		{"no due id:01B", BucketNoDate},
		{"overdue due:2026-06-01 id:01C", BucketOverdue},
		{"today due:2026-06-09 id:01D", BucketToday},
		{"today timed due:2026-06-09T17:00 id:01E", BucketToday}, // the bug: was "This week"
		{"this week due:2026-06-12 id:01F", BucketThisWeek},
		{"this week timed due:2026-06-12T09:00 id:01G", BucketThisWeek},
		{"later due:2026-07-01 id:01H", BucketLater},
	}
	for _, c := range cases {
		if got := DueBucket(mk(c.line), today, weekEnd); got != c.want {
			t.Errorf("%q: got %q want %q", c.line, got, c.want)
		}
	}
}
