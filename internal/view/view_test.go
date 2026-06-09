package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing views file should not error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil map, got %#v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := map[string]Spec{
		"overdue": {Status: "open", Sort: "due", Tag: "urgent"},
		"work":    {Project: "work", ShowBlocked: true, Tree: true},
		"default": {}, // the bare "all open" view
	}
	if err := Save(dir, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	// File is real JSON on disk.
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("views file missing after save: %v", err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("want %d views, got %d", len(in), len(out))
	}
	for name, want := range in {
		if out[name] != want {
			t.Errorf("view %q round-trip: got %#v, want %#v", name, out[name], want)
		}
	}
}

func TestValidName(t *testing.T) {
	ok := []string{"work", "this-week", "p1_urgent", "v2"}
	for _, n := range ok {
		if err := ValidName(n); err != nil {
			t.Errorf("ValidName(%q) should pass: %v", n, err)
		}
	}
	bad := []string{"", "save", "list", "rm", "two words", "a/b", "star*", "q?"}
	for _, n := range bad {
		if err := ValidName(n); err == nil {
			t.Errorf("ValidName(%q) should fail", n)
		}
	}
}

func TestArgsAndSummary(t *testing.T) {
	s := Spec{Status: "open", Tag: "urgent", Sort: "due", All: true, ShowBlocked: true, Tree: true, Project: "web"}
	got := strings.Join(s.Args(), " ")
	want := "--status open --tag urgent --project web --sort due --all --show-blocked --tree"
	if got != want {
		t.Errorf("Args() = %q, want %q", got, want)
	}
	if (Spec{}).Summary() != "(all open tasks)" {
		t.Errorf("empty Summary() = %q", (Spec{}).Summary())
	}
}
