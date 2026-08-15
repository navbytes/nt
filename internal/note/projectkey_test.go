package note

import (
	"reflect"
	"testing"
)

// One fold for every surface that compares projects: trimmed + lowercased.
func TestSameProjectFoldsCaseAndSpace(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"WTCockpit", "wtcockpit", true},
		{" webhookd ", "webhookd", true},
		{"webhookd", "tripto", false},
		{"", "", false}, // empty never matches — a "" filter must not select every unscoped note
		{"", "webhookd", false},
	}
	for _, c := range cases {
		if got := SameProject(c.a, c.b); got != c.want {
			t.Errorf("SameProject(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// SetProject is the write-side of Project: set, replace, clear — always
// preserving adjacent unmodeled frontmatter in Extra.
func TestSetProjectSetReplaceClear(t *testing.T) {
	n := &Note{Extra: []string{"status: stable"}}

	n.SetProject("webhookd")
	if n.Project() != "webhookd" {
		t.Fatalf("set: Project() = %q", n.Project())
	}

	n.SetProject("tripto")
	if n.Project() != "tripto" {
		t.Fatalf("replace: Project() = %q", n.Project())
	}
	if !reflect.DeepEqual(n.Extra, []string{"status: stable", "project: tripto"}) {
		t.Fatalf("replace must edit in place, not append a second key: %v", n.Extra)
	}

	n.SetProject("")
	if n.Project() != "" {
		t.Fatalf("clear: Project() = %q", n.Project())
	}
	if !reflect.DeepEqual(n.Extra, []string{"status: stable"}) {
		t.Fatalf("clear must remove only the project line: %v", n.Extra)
	}
}
