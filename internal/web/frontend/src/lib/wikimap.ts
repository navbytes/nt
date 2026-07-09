// The second mind-map source: instead of one note's internal outline, root on
// the current note and radiate outward along [[wikilinks]] — a shortest-path
// tree carved out of the note↔note graph. It reuses the same OutlineNode shape
// (so MindMap renders it unchanged) and the same node cap; only the tree
// construction differs. Task nodes and task/dependency edges are excluded: this
// is deliberately a *notes* map (the /graph route already shows the full mesh).

import type { GraphData, GraphNode } from "./api-types";
import { maxOutlineNodes, type OutlineNode, type ParsedOutline } from "./outline";

// wikiTree builds a breadth-first tree from rootId over undirected wikilink
// edges, so every reachable note appears once at its shortest distance (≤
// maxDepth hops). Neighbors are visited in title order for a stable layout.
export function wikiTree(data: GraphData, rootId: string, maxDepth: number): ParsedOutline {
  const byId = new Map<string, GraphNode>();
  for (const n of data.nodes) if (n.kind === "note") byId.set(n.id, n);

  // Adjacency over wikilink edges between two note endpoints only.
  const adj = new Map<string, Set<string>>();
  const ensure = (id: string) => {
    let s = adj.get(id);
    if (!s) adj.set(id, (s = new Set()));
    return s;
  };
  for (const l of data.links) {
    if (l.kind && l.kind !== "wikilink") continue;
    const s = data.nodes[l.s];
    const t = data.nodes[l.t];
    if (!s || !t || s.id === t.id || s.kind !== "note" || t.kind !== "note") continue;
    ensure(s.id).add(t.id);
    ensure(t.id).add(s.id);
  }

  const rootNode = byId.get(rootId);
  const root: OutlineNode = {
    id: "root",
    text: rootNode?.title ?? rootId,
    kind: "root",
    level: 0,
    anchor: rootNode?.url,
    children: [],
  };
  const out: ParsedOutline = { root, count: 1, truncated: false };
  if (!rootNode) return out;

  const title = (id: string) => byId.get(id)?.title ?? "";
  const visited = new Set<string>([rootId]);
  let frontier: Array<{ id: string; node: OutlineNode }> = [{ id: rootId, node: root }];

  for (let d = 0; d < maxDepth; d++) {
    const next: Array<{ id: string; node: OutlineNode }> = [];
    for (const { id, node } of frontier) {
      const nbrs = [...(adj.get(id) ?? [])].sort((a, b) => title(a).localeCompare(title(b)));
      for (const nb of nbrs) {
        if (visited.has(nb)) continue;
        visited.add(nb);
        if (out.count >= maxOutlineNodes) {
          out.truncated = true;
          continue;
        }
        const gn = byId.get(nb)!;
        const child: OutlineNode = {
          id: `${node.id}.${node.children.length}`,
          text: gn.title,
          kind: "heading",
          level: 1,
          anchor: gn.url, // "/n/<handle>" — navigate on activate
          children: [],
        };
        node.children.push(child);
        out.count++;
        next.push({ id: nb, node: child });
      }
    }
    frontier = next;
    if (frontier.length === 0) break;
  }
  return out;
}
