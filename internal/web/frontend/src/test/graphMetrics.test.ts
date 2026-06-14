import { describe, it, expect } from "vitest";
import { bfsDepths, pageRank } from "../lib/graphMetrics";
import { buildAdjacency } from "../lib/graph";
import type { GraphData } from "../lib/api-types";

function node(id: string) {
  return { id, kind: "note", title: id, url: `/n/${id}`, folder: "", source: "cli", tags: [], deg: 0 };
}

// chain: a - b - c - d   plus an isolated node e
const data: GraphData = {
  nodes: [node("a"), node("b"), node("c"), node("d"), node("e")],
  links: [
    { s: 0, t: 1 },
    { s: 1, t: 2 },
    { s: 2, t: 3 },
  ],
};
const adj = buildAdjacency(data);

describe("bfsDepths", () => {
  it("assigns hop distance from the root (root is depth 0)", () => {
    const d = bfsDepths("a", adj);
    expect(d.get("a")).toBe(0);
    expect(d.get("b")).toBe(1);
    expect(d.get("c")).toBe(2);
    expect(d.get("d")).toBe(3);
  });

  it("omits nodes not reachable from the root", () => {
    expect(bfsDepths("a", adj).has("e")).toBe(false);
  });

  it("respects maxDepth", () => {
    const d = bfsDepths("a", adj, 1);
    expect([...d.keys()].sort()).toEqual(["a", "b"]);
  });

  it("returns an empty map for an unknown root", () => {
    expect(bfsDepths("zzz", adj).size).toBe(0);
  });
});

describe("pageRank", () => {
  it("scores every node and sums to ~1", () => {
    const pr = pageRank(adj);
    expect(pr.size).toBe(5);
    const sum = [...pr.values()].reduce((a, b) => a + b, 0);
    expect(sum).toBeCloseTo(1, 5);
  });

  it("ranks central nodes above leaves", () => {
    // In the chain a-b-c-d, the interior nodes b and c outrank the endpoints.
    const pr = pageRank(adj);
    expect(pr.get("b")!).toBeGreaterThan(pr.get("a")!);
    expect(pr.get("c")!).toBeGreaterThan(pr.get("d")!);
  });

  it("keeps isolated nodes at a positive baseline (no zero/NaN)", () => {
    const pr = pageRank(adj);
    expect(pr.get("e")!).toBeGreaterThan(0);
    for (const v of pr.values()) expect(Number.isFinite(v)).toBe(true);
  });

  it("handles an empty graph", () => {
    expect(pageRank(new Map()).size).toBe(0);
  });
});
