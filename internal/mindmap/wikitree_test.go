package mindmap

import (
	"testing"

	"github.com/navbytes/nt/internal/note"
)

func TestWikiTree(t *testing.T) {
	// A links B and C; B links back to A. C is a leaf.
	a := &note.Note{Path: "a.md", Title: "A", Body: "see [[B]] and [[C]]"}
	b := &note.Note{Path: "b.md", Title: "B", Body: "back to [[A]]"}
	c := &note.Note{Path: "c.md", Title: "C", Body: "leaf"}
	notes := []*note.Note{a, b, c}

	root := WikiTree(a, notes, nil, 3)
	if got := shape(root); got != "A[B,C]" {
		t.Fatalf("wiki tree wrong: %q", got)
	}
}

func TestWikiTreeDepthLimit(t *testing.T) {
	// A—B—C chain; depth 1 from A should reach only B.
	a := &note.Note{Path: "a.md", Title: "A", Body: "[[B]]"}
	b := &note.Note{Path: "b.md", Title: "B", Body: "[[C]]"}
	c := &note.Note{Path: "c.md", Title: "C", Body: "leaf"}
	root := WikiTree(a, []*note.Note{a, b, c}, nil, 1)
	if got := shape(root); got != "A[B]" {
		t.Fatalf("depth 1 should reach only B: %q", got)
	}
}

func TestWikiTreeNoLinks(t *testing.T) {
	a := &note.Note{Path: "a.md", Title: "A", Body: "no links here"}
	root := WikiTree(a, []*note.Note{a}, nil, 3)
	if len(root.Children) != 0 {
		t.Fatalf("expected no children, got %d", len(root.Children))
	}
}
