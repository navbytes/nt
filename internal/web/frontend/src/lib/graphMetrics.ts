// Pure graph metrics over the undirected adjacency map (graph.ts buildAdjacency).
// Kept here — separate from the canvas renderer — so they're unit-testable and
// computed once per dataset, then read O(1) by the hot draw loop.

// bfsDepths returns each reachable node's hop-distance from rootId (the root is
// depth 0), via BFS. Drives the radial/ego layout: a node's ring is its depth.
// Nodes not reachable from the root are simply absent from the map.
export function bfsDepths(
  rootId: string,
  adj: Map<string, Set<string>>,
  maxDepth = Infinity,
): Map<string, number> {
  const depth = new Map<string, number>();
  if (!adj.has(rootId)) return depth;
  depth.set(rootId, 0);
  let frontier: string[] = [rootId];
  let d = 0;
  while (frontier.length && d < maxDepth) {
    d++;
    const next: string[] = [];
    for (const id of frontier) {
      for (const nb of adj.get(id) ?? []) {
        if (!depth.has(nb)) {
          depth.set(nb, d);
          next.push(nb);
        }
      }
    }
    frontier = next;
  }
  return depth;
}

// pageRank scores structural importance via the standard power-iteration on the
// undirected graph (each edge contributes both directions). Returns a map of
// id → score where the scores sum to ~1. Dangling nodes (no neighbors) keep a
// uniform baseline so isolated notes don't collapse to zero. Cheap enough to run
// on every dataset change (O(iterations · edges)); we cap iterations rather than
// test for convergence so the cost is bounded and predictable.
export function pageRank(
  adj: Map<string, Set<string>>,
  { damping = 0.85, iterations = 40 }: { damping?: number; iterations?: number } = {},
): Map<string, number> {
  const ids = [...adj.keys()];
  const n = ids.length;
  const rank = new Map<string, number>();
  if (n === 0) return rank;
  const base = 1 / n;
  for (const id of ids) rank.set(id, base);

  for (let it = 0; it < iterations; it++) {
    const next = new Map<string, number>();
    let dangling = 0; // mass from nodes with no out-edges, redistributed uniformly
    for (const id of ids) {
      next.set(id, (1 - damping) / n);
      if ((adj.get(id)?.size ?? 0) === 0) dangling += (rank.get(id) ?? 0);
    }
    const danglingShare = (damping * dangling) / n;
    for (const id of ids) {
      const nbrs = adj.get(id);
      const share = (rank.get(id) ?? 0) / (nbrs?.size || 1);
      if (nbrs) for (const nb of nbrs) next.set(nb, (next.get(nb) ?? 0) + damping * share);
      if (danglingShare) next.set(id, (next.get(id) ?? 0) + danglingShare);
    }
    for (const id of ids) rank.set(id, next.get(id) ?? 0);
  }
  return rank;
}
