import { describe, it, expect } from "vitest";
import { toForceGraph } from "../lib/graph";
import type { GraphData } from "../lib/api-types";

function node(id: string, folder = "", deg = 0) {
  return { id, title: id.toUpperCase(), url: `/n/${id}`, folder, source: "cli", tags: [], deg };
}

describe("toForceGraph", () => {
  it("maps index-based links to id-based source/target", () => {
    const data: GraphData = { nodes: [node("a", "", 1), node("b", "x", 1)], links: [{ s: 0, t: 1 }] };
    const fg = toForceGraph(data);
    expect(fg.nodes.map((n) => n.id)).toEqual(["a", "b"]);
    expect(fg.links).toEqual([{ source: "a", target: "b" }]);
  });

  it("carries folder + degree through for coloring/sizing", () => {
    const fg = toForceGraph({ nodes: [node("a", "docs", 4)], links: [] });
    expect(fg.nodes[0]).toMatchObject({ folder: "docs", deg: 4, url: "/n/a" });
  });

  it("drops links with out-of-range endpoints", () => {
    const data: GraphData = { nodes: [node("a")], links: [{ s: 0, t: 9 }] };
    expect(toForceGraph(data).links).toHaveLength(0);
  });
});
