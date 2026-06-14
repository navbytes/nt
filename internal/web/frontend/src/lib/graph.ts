import type { GraphData } from "./api-types";

// force-graph wants links that reference node ids; our API sends link endpoints
// as indices into the nodes array (compact wire format). These pure mappings are
// unit-tested (the renderer itself needs a canvas, so this is the verifiable
// seam between the API and force-graph).

export interface FGNode {
  id: string;
  kind: string; // "note" | "task"
  title: string;
  url: string;
  folder: string;
  source: string;
  tags: string[];
  deg: number;
  // force-graph mutates these in place during layout.
  x?: number;
  y?: number;
  fx?: number | undefined;
  fy?: number | undefined;
}

export interface FGLink {
  source: string;
  target: string;
  kind?: string; // relationship: "wikilink" | "task" | "parent" | "blocks" | "discovered"
}

export function toForceGraph(data: GraphData): { nodes: FGNode[]; links: FGLink[] } {
  const nodes: FGNode[] = data.nodes.map((n) => ({
    id: n.id,
    kind: n.kind || "note",
    title: n.title,
    url: n.url,
    folder: n.folder,
    source: n.source,
    tags: n.tags ?? [],
    deg: n.deg,
  }));
  const links: FGLink[] = [];
  for (const l of data.links) {
    const s = data.nodes[l.s];
    const t = data.nodes[l.t];
    if (s && t) links.push({ source: s.id, target: t.id, kind: l.kind });
  }
  return { nodes, links };
}

// buildAdjacency precomputes an undirected neighbor map once per dataset, so
// hover-highlight, local-graph BFS, and keyboard traversal are all O(1) lookups
// instead of re-scanning every link per node per frame.
export function buildAdjacency(data: GraphData): Map<string, Set<string>> {
  const adj = new Map<string, Set<string>>();
  for (const n of data.nodes) adj.set(n.id, new Set());
  for (const l of data.links) {
    const s = data.nodes[l.s];
    const t = data.nodes[l.t];
    if (!s || !t || s.id === t.id) continue;
    adj.get(s.id)!.add(t.id);
    adj.get(t.id)!.add(s.id);
  }
  return adj;
}

// nodesWithinDepth returns the set of node ids reachable from rootId within
// maxDepth hops (inclusive of the root), via BFS over the undirected adjacency.
// This is the local-graph "depth slider" computation.
export function nodesWithinDepth(
  rootId: string,
  adj: Map<string, Set<string>>,
  maxDepth: number,
): Set<string> {
  const seen = new Set<string>();
  if (!adj.has(rootId)) return seen;
  seen.add(rootId);
  let frontier: string[] = [rootId];
  for (let d = 0; d < maxDepth; d++) {
    const next: string[] = [];
    for (const id of frontier) {
      for (const nb of adj.get(id) ?? []) {
        if (!seen.has(nb)) {
          seen.add(nb);
          next.push(nb);
        }
      }
    }
    frontier = next;
    if (frontier.length === 0) break;
  }
  return seen;
}

// linkEndId normalises a force-graph link endpoint, which may be a bare id
// (before layout) or a resolved node object (after layout binds them).
export function linkEndId(end: unknown): string {
  return typeof end === "object" && end !== null ? (end as FGNode).id : (end as string);
}
