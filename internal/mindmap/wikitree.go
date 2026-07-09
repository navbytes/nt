package mindmap

import (
	"sort"

	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// WikiTree builds a breadth-first mind-map tree rooted at `root`, radiating along
// resolved [[wikilinks]] between notes (undirected, up to maxDepth hops). It's
// the terminal/agent counterpart to the web app's "Links" map: notes only, each
// note appears once at its shortest distance, neighbors visited in title order.
func WikiTree(root *note.Note, notes []*note.Note, doc *task.Doc, maxDepth int) *Node {
	byPath := make(map[string]*note.Note, len(notes))
	for _, n := range notes {
		byPath[n.Path] = n
	}
	title := func(path string) string {
		if n := byPath[path]; n != nil {
			return n.Title
		}
		return path
	}

	// Undirected adjacency over resolved note→note wikilinks.
	adj := make(map[string]map[string]bool)
	connect := func(a, b string) {
		if adj[a] == nil {
			adj[a] = make(map[string]bool)
		}
		if adj[b] == nil {
			adj[b] = make(map[string]bool)
		}
		adj[a][b] = true
		adj[b][a] = true
	}
	for _, n := range notes {
		for _, raw := range links.Wikilinks(n.Body) {
			if it, ok := links.Resolve(raw, doc, notes); ok && it.Kind == "note" && it.Path != n.Path {
				connect(n.Path, it.Path)
			}
		}
	}

	rootNode := &Node{Text: firstNonEmpty(sanitize(root.Title), "(untitled)")}
	visited := map[string]bool{root.Path: true}
	count := 1
	type frame struct {
		path string
		node *Node
	}
	frontier := []frame{{root.Path, rootNode}}

	for d := 0; d < maxDepth; d++ {
		var next []frame
		for _, f := range frontier {
			nbrs := make([]string, 0, len(adj[f.path]))
			for p := range adj[f.path] {
				nbrs = append(nbrs, p)
			}
			sort.Slice(nbrs, func(i, j int) bool { return title(nbrs[i]) < title(nbrs[j]) })
			for _, p := range nbrs {
				if visited[p] {
					continue
				}
				visited[p] = true
				if count >= MaxNodes {
					break
				}
				child := &Node{Text: sanitize(title(p))}
				f.node.Children = append(f.node.Children, child)
				count++
				next = append(next, frame{p, child})
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}
	return rootNode
}
