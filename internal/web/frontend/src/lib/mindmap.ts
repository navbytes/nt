// Pure radial layout for the outline mind map. force-graph (the /graph route)
// simulates a physics layout, but a mind map is a *tree* — a deterministic
// radial placement reads far better and needs no simulation, no new dependency,
// and stays unit-testable (the SVG renderer just consumes these coordinates).
//
// The tree is placed on concentric rings: the root sits at the origin, and depth
// d lands on radius d·ringGap. Angles are assigned by a single post-order pass —
// each leaf takes the next slice of the circle, and each internal node centers
// over the angular span of its visible children (the classic tidy-radial trick).
// A node in `collapsed` keeps its own dot but contributes no descendants, so the
// layout reflows to fill the freed space.

import type { OutlineNode } from "./outline";

// mapSearch computes the note page's query string for a given map state, so the
// view is a shareable, reload-surviving deep link (?view=map[&map=links]). It
// preserves any other params already present (e.g. ?missing=1) and returns the
// query WITHOUT a leading "?". Pure, so the URL contract is unit-tested.
export function mapSearch(
  currentSearch: string,
  mapView: boolean,
  mapSource: "outline" | "links",
): string {
  const q = new URLSearchParams(currentSearch);
  if (mapView) {
    q.set("view", "map");
    if (mapSource === "links") q.set("map", "links");
    else q.delete("map");
  } else {
    q.delete("view");
    q.delete("map");
  }
  return q.toString();
}

export interface MapNode {
  id: string;
  text: string;
  kind: OutlineNode["kind"];
  level: number;
  anchor?: string;
  depth: number; // ring index from root
  x: number;
  y: number;
  angle: number; // radians, for label anchoring (left/right of the spoke)
  hasChildren: boolean; // has children in the full tree (may be collapsed)
  collapsed: boolean;
  hiddenCount: number; // descendants hidden because this node is collapsed (0 if expanded)
}

export interface MapEdge {
  from: string;
  to: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

export interface MapLayout {
  nodes: MapNode[];
  edges: MapEdge[];
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

export interface LayoutOpts {
  ringGap?: number; // px between concentric rings
  collapsed?: ReadonlySet<string>;
}

const DEFAULT_RING_GAP = 170;

// countLeaves returns the number of leaf slots a node occupies given the collapse
// set — a collapsed node (or a genuine leaf) counts as one slot; otherwise it's
// the sum of its children's slots. This drives even angular distribution.
function countLeaves(node: OutlineNode, collapsed: ReadonlySet<string>): number {
  if (collapsed.has(node.id) || node.children.length === 0) return 1;
  let n = 0;
  for (const c of node.children) n += countLeaves(c, collapsed);
  return n;
}

function subtreeSize(node: OutlineNode): number {
  let n = 0;
  for (const c of node.children) n += 1 + subtreeSize(c);
  return n;
}

// radialLayout positions the (post-collapse) visible tree. Returns nodes, edges,
// and the tight bounding box so the caller can fit the SVG viewBox.
export function radialLayout(root: OutlineNode, opts: LayoutOpts = {}): MapLayout {
  const ringGap = opts.ringGap ?? DEFAULT_RING_GAP;
  const collapsed = opts.collapsed ?? new Set<string>();
  const nodes: MapNode[] = [];
  const edges: MapEdge[] = [];

  const totalLeaves = Math.max(1, countLeaves(root, collapsed));
  const slice = (2 * Math.PI) / totalLeaves;
  let leafCursor = 0;

  // Post-order assignment: descend to leaves to claim angular slots, then set an
  // internal node's angle to the mean of its children. Returns the node's angle.
  const place = (node: OutlineNode, depth: number): number => {
    const isCollapsed = collapsed.has(node.id);
    let angle: number;
    if (isCollapsed || node.children.length === 0) {
      angle = (leafCursor + 0.5) * slice;
      leafCursor++;
    } else {
      let sum = 0;
      for (const c of node.children) sum += place(c, depth + 1);
      angle = sum / node.children.length;
    }
    // Push every ring outward by a constant so the crowded inner ring (depth 1)
    // gets ~25% more circumference — relieving label overlap between many H2s
    // without spreading a sparse map too thin.
    const r = depth === 0 ? 0 : ringGap * (depth + 0.25);
    const mn: MapNode = {
      id: node.id,
      text: node.text,
      kind: node.kind,
      level: node.level,
      anchor: node.anchor,
      depth,
      x: r * Math.cos(angle - Math.PI / 2), // −90° so the tree opens upward-ish
      y: r * Math.sin(angle - Math.PI / 2),
      angle: angle - Math.PI / 2,
      hasChildren: node.children.length > 0,
      collapsed: isCollapsed,
      hiddenCount: isCollapsed ? subtreeSize(node) : 0,
    };
    nodes.push(mn);
    return angle;
  };
  place(root, 0);

  // Edges connect each node to its visible children (skip descendants of a
  // collapsed node). Index by id for endpoint coordinates.
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const link = (node: OutlineNode) => {
    if (collapsed.has(node.id)) return;
    const p = byId.get(node.id);
    if (!p) return;
    for (const c of node.children) {
      const cn = byId.get(c.id);
      if (!cn) continue;
      edges.push({ from: node.id, to: c.id, x1: p.x, y1: p.y, x2: cn.x, y2: cn.y });
      link(c);
    }
  };
  link(root);

  let minX = 0;
  let minY = 0;
  let maxX = 0;
  let maxY = 0;
  for (const n of nodes) {
    if (n.x < minX) minX = n.x;
    if (n.y < minY) minY = n.y;
    if (n.x > maxX) maxX = n.x;
    if (n.y > maxY) maxY = n.y;
  }
  return { nodes, edges, minX, minY, maxX, maxY };
}
