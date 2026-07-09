package mindmap

import (
	"strings"
	"testing"
)

// shape renders the tree as "text[child,child]" for compact structural asserts.
func shape(n *Node) string {
	if len(n.Children) == 0 {
		return n.Text
	}
	parts := make([]string, len(n.Children))
	for i, c := range n.Children {
		parts[i] = shape(c)
	}
	return n.Text + "[" + strings.Join(parts, ",") + "]"
}

func TestOutlineHeadingsNest(t *testing.T) {
	body := "## A\n### A1\n### A2\n## B\n"
	got := shape(Outline(body, "T"))
	if want := "T[A[A1,A2],B]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOutlineListsUnderHeading(t *testing.T) {
	body := "## Goals\n- Ship\n  - Auth\n  - Billing\n- Grow\n"
	got := shape(Outline(body, "T"))
	if want := "T[Goals[Ship[Auth,Billing],Grow]]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOutlinePreHeadingListUnderRoot(t *testing.T) {
	body := "- intro\n## A\n"
	got := shape(Outline(body, "T"))
	if want := "T[intro,A]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOutlineLooseListStaysNested(t *testing.T) {
	body := "- A\n  - A1\n\n  - A2\n"
	if got := shape(Outline(body, "T")); got != "T[A[A1,A2]]" {
		t.Fatalf("loose list flattened: %q", got)
	}
}

func TestOutlineSetextHeadings(t *testing.T) {
	body := "Overview\n===\n\nDetails\n---\n"
	if got := shape(Outline(body, "T")); got != "T[Overview[Details]]" {
		t.Fatalf("setext headings wrong: %q", got)
	}
}

func TestOutlineSkipsFencedCode(t *testing.T) {
	body := "## A\n```\n## not a heading\n- not a bullet\n```\n- real\n"
	got := shape(Outline(body, "T"))
	if want := "T[A[real]]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOutlineShallowHeadingPops(t *testing.T) {
	body := "## A\n#### deep\n## B\n"
	got := shape(Outline(body, "T"))
	if want := "T[A[deep],B]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSanitizeStripsMarkdownAndDelimiters(t *testing.T) {
	body := "## **Bold** [[Target|Alias]] and Auth (OAuth)\n"
	n := Outline(body, "T")
	if got := n.Children[0].Text; got != "Bold Alias and Auth OAuth" {
		t.Fatalf("sanitize got %q", got)
	}
}

func TestOutlineStripsDuplicateTitleH1(t *testing.T) {
	// nt prepends "# Title" for bodies without a heading; it must not become a
	// child that duplicates the root.
	body := "# My Note\n\n## A\n- x\n"
	got := shape(Outline(body, "My Note"))
	if want := "My Note[A[x]]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// A genuinely different first H1 is kept (and an H2 nests under it).
	got2 := shape(Outline("# Intro\n## A\n", "My Note"))
	if want := "My Note[Intro[A]]"; got2 != want {
		t.Fatalf("got %q want %q", got2, want)
	}
}

func TestOutlineEmptyWhenOnlyTitleAndProse(t *testing.T) {
	// The "flat note" case: title H1 + a paragraph → nothing to map.
	root := Outline("# Flat\n\njust a paragraph\n", "Flat")
	if len(root.Children) != 0 {
		t.Fatalf("expected no children, got %d", len(root.Children))
	}
}

func TestMermaidRootAndIndent(t *testing.T) {
	body := "## Goals\n- Ship\n"
	out := Mermaid(Outline(body, "Project Alpha"), 0)
	for _, want := range []string{"mindmap\n", "  root((Project Alpha))\n", "    Goals\n", "      Ship\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mermaid output missing %q:\n%s", want, out)
		}
	}
}

func TestMermaidDepthLimit(t *testing.T) {
	body := "## A\n- one\n  - two\n"
	out := Mermaid(Outline(body, "T"), 1) // root(0) + A(1), stop before list items
	if strings.Contains(out, "one") || strings.Contains(out, "two") {
		t.Fatalf("depth 1 should stop at headings:\n%s", out)
	}
	if !strings.Contains(out, "    A\n") {
		t.Fatalf("depth 1 should include heading A:\n%s", out)
	}
}

func TestFenceWraps(t *testing.T) {
	out := Fence(Mermaid(Outline("## A\n", "T"), 0))
	if !strings.HasPrefix(out, "```mermaid\n") || !strings.HasSuffix(out, "```\n") {
		t.Fatalf("fence wrapper wrong:\n%s", out)
	}
}

func TestOutlineNodeCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxNodes+50; i++ {
		b.WriteString("- item\n")
	}
	root := Outline(b.String(), "T")
	// Count nodes; must not exceed the cap.
	var count func(n *Node) int
	count = func(n *Node) int {
		c := 1
		for _, k := range n.Children {
			c += count(k)
		}
		return c
	}
	if got := count(root); got > MaxNodes {
		t.Fatalf("node count %d exceeds cap %d", got, MaxNodes)
	}
}
