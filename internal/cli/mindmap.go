package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/navbytes/nt/internal/mindmap"
	"github.com/navbytes/nt/internal/note"
)

// cmdMindmap emits a Mermaid `mindmap` diagram of a note's outline — the
// terminal/agent counterpart to the web app's interactive mind map. The output
// is a paste-ready ```mermaid``` fence (nt's own web viewer, Obsidian, and
// GitHub all render it), so an agent can capture a topic's structure into a note.
func cmdMindmap(args []string) int {
	fs := flag.NewFlagSet("mindmap", flag.ContinueOnError)
	depth := fs.Int("depth", 0, "limit to N levels below the root (0 = all)")
	noFence := fs.Bool("no-fence", false, "emit the raw mindmap, without the ```mermaid``` code fence")

	flags, positional := splitArgs(args, map[string]bool{"no-fence": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("mindmap: need a note handle (slug/title/id)"))
	}

	e, ok := engine()
	if !ok {
		return 1
	}
	notes, _ := note.List(e.S)
	handle := strings.Join(positional, " ")
	n, err := resolveNote(notes, handle)
	if err != nil {
		return fail(fmt.Errorf("mindmap: no note %q — try `nt notes` to list them", handle))
	}

	root := mindmap.Outline(n.Body, n.Title)
	if len(root.Children) == 0 {
		return fail(fmt.Errorf("mindmap: %q has no headings or lists to map", n.Title))
	}

	out := mindmap.Mermaid(root, *depth)
	if !*noFence {
		out = mindmap.Fence(out)
	}
	fmt.Print(out)
	return 0
}
