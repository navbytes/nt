package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/navbytes/nt/internal/mindmap"
	"github.com/navbytes/nt/internal/note"
)

// cmdMindmap emits a Mermaid `mindmap` diagram of a note — the terminal/agent
// counterpart to the web app's interactive mind map. Two sources: the note's own
// outline (default) or the notes it links to via [[wikilinks]] (--links). Output
// is a paste-ready ```mermaid``` fence (nt's web viewer, Obsidian, and GitHub all
// render it) or, with --format json, the raw tree for programmatic use.
func cmdMindmap(args []string) int {
	fs := flag.NewFlagSet("mindmap", flag.ContinueOnError)
	depth := fs.Int("depth", 0, "limit to N levels below the root (0 = all; default 3 for --links)")
	noFence := fs.Bool("no-fence", false, "emit the raw mindmap, without the ```mermaid``` code fence")
	useLinks := fs.Bool("links", false, "map the notes this one links to via [[wikilinks]] instead of its outline")
	format := fs.String("format", "mermaid", "output format: mermaid | json")

	flags, positional := splitArgs(args, map[string]bool{"no-fence": true, "links": true})
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) == 0 {
		return usageErr(fmt.Errorf("mindmap: need a note handle (slug/title/id)"))
	}
	if *format != "mermaid" && *format != "json" {
		return usageErr(fmt.Errorf("mindmap: --format must be mermaid or json"))
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

	var root *mindmap.Node
	if *useLinks {
		d, _ := e.Read()
		hops := *depth
		if hops == 0 {
			hops = 3 // a sensible default radius for the link web
		}
		root = mindmap.WikiTree(n, notes, d, hops)
		if len(root.Children) == 0 {
			return fail(fmt.Errorf("mindmap: %q links to no other notes — add [[wikilinks]] first", n.Title))
		}
	} else {
		root = mindmap.Outline(n.Body, n.Title)
		if len(root.Children) == 0 {
			return fail(fmt.Errorf("mindmap: %q has no headings or lists to map", n.Title))
		}
	}

	if *format == "json" {
		b, _ := json.MarshalIndent(root, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	out := mindmap.Mermaid(root, *depth)
	if !*noFence {
		out = mindmap.Fence(out)
	}
	fmt.Print(out)
	return 0
}
