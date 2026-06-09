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
