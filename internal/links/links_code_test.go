package links

import "testing"

// Documenting nt's own link syntax inside nt used to mint real dangling links
// from the examples, which nt doctor then reported forever with no way to quote
// a [[…]] verbatim. Markdown's code convention is the escape hatch.
func TestWikilinksIgnoresCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain link counts", "see [[real-note]] here", []string{"real-note"}},
		{"inline code span quoted", "write `[[note-slug]]` to link", nil},
		{"double backtick span quoted", "use ``[[x]]`` verbatim", nil},
		{"fenced block quoted", "text\n```\n[[example]]\n```\nmore", nil},
		{"tilde fence quoted", "text\n~~~\n[[example]]\n~~~\n", nil},
		{"unclosed backtick is not code", "a ` [[still-a-link]]", []string{"still-a-link"}},
		{"mixed: prose link survives alongside a quoted one",
			"real [[keeper]] and quoted `[[skipme]]`", []string{"keeper"}},
		{"fence does not swallow the rest of the doc",
			"```\n[[hidden]]\n```\nthen [[visible]]", []string{"visible"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Wikilinks(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}
