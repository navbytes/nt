import { describe, it, expect } from "vitest";
import { radialLayout } from "../lib/mindmap";
import { parseOutline } from "../lib/outline";

const tree = () => parseOutline("<h2>A</h2><h3>A1</h3><h3>A2</h3><h2>B</h2>", "T").root;

describe("radialLayout", () => {
  it("places the root at the origin", () => {
    const { nodes } = radialLayout(tree());
    const root = nodes.find((n) => n.id === "root")!;
    expect(root.x).toBeCloseTo(0);
    expect(root.y).toBeCloseTo(0);
    expect(root.depth).toBe(0);
  });

  it("puts each node on the ring for its depth", () => {
    const { nodes } = radialLayout(tree(), { ringGap: 100 });
    const byId = new Map(nodes.map((n) => [n.id, n]));
    const a = byId.get("root.0")!; // depth 1
    const a1 = byId.get("root.0.0")!; // depth 2
    expect(Math.hypot(a.x, a.y)).toBeCloseTo(100);
    expect(Math.hypot(a1.x, a1.y)).toBeCloseTo(200);
  });

  it("emits an edge per parent→child and finite coordinates", () => {
    const { nodes, edges } = radialLayout(tree());
    expect(edges).toHaveLength(nodes.length - 1); // a tree: |E| = |V| − 1
    for (const e of edges) {
      expect(Number.isFinite(e.x1) && Number.isFinite(e.y2)).toBe(true);
    }
  });

  it("collapsing a node hides its subtree and reflows", () => {
    const full = radialLayout(tree());
    const collapsed = radialLayout(tree(), { collapsed: new Set(["root.0"]) });
    // A's two children (A1, A2) drop out.
    expect(collapsed.nodes.length).toBe(full.nodes.length - 2);
    const a = collapsed.nodes.find((n) => n.id === "root.0")!;
    expect(a.collapsed).toBe(true);
    expect(a.hiddenCount).toBe(2);
    // No edge should reference a hidden child.
    expect(collapsed.edges.some((e) => e.to === "root.0.0")).toBe(false);
  });

  it("marks internal nodes as having children", () => {
    const { nodes } = radialLayout(tree());
    expect(nodes.find((n) => n.id === "root.0")!.hasChildren).toBe(true);
    expect(nodes.find((n) => n.id === "root.1")!.hasChildren).toBe(false); // B is a leaf
  });
});
