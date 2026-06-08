import { describe, it, expect } from "vitest";
import { toForceGraph, buildAdjacency, nodesWithinDepth, linkEndId } from "../lib/graph";
import type { GraphData } from "../lib/api-types";

function node(id: string, folder = "", deg = 0, tags: string[] = [], source = "cli") {
  return { id, kind: "note", title: id.toUpperCase(), url: `/n/${id}`, folder, source, tags, deg };
}

describe("toForceGraph", () => {
  it("maps index-based links to id-based source/target", () => {
    const data: GraphData = { nodes: [node("a", "", 1), node("b", "x", 1)], links: [{ s: 0, t: 1 }] };
    const fg = toForceGraph(data);
    expect(fg.nodes.map((n) => n.id)).toEqual(["a", "b"]);
    expect(fg.links).toEqual([{ source: "a", target: "b" }]);
  });

  it("carries folder + degree + tags + source through for coloring/sizing", () => {
    const fg = toForceGraph({ nodes: [node("a", "docs", 4, ["x"], "claude")], links: [] });
    expect(fg.nodes[0]).toMatchObject({ folder: "docs", deg: 4, url: "/n/a", tags: ["x"], source: "claude" });
  });

  it("drops links with out-of-range endpoints", () => {
    const data: GraphData = { nodes: [node("a")], links: [{ s: 0, t: 9 }] };
    expect(toForceGraph(data).links).toHaveLength(0);
  });
});

describe("buildAdjacency", () => {
  it("builds an undirected neighbor map", () => {
    const data: GraphData = {
      nodes: [node("a"), node("b"), node("c")],
      links: [{ s: 0, t: 1 }, { s: 1, t: 2 }],
    };
    const adj = buildAdjacency(data);
    expect([...adj.get("a")!]).toEqual(["b"]);
    expect([...adj.get("b")!].sort()).toEqual(["a", "c"]);
    expect([...adj.get("c")!]).toEqual(["b"]);
  });

  it("ignores self-links and out-of-range endpoints", () => {
    const data: GraphData = {
      nodes: [node("a"), node("b")],
      links: [{ s: 0, t: 0 }, { s: 0, t: 5 }],
    };
    const adj = buildAdjacency(data);
    expect(adj.get("a")!.size).toBe(0);
    expect(adj.get("b")!.size).toBe(0);
  });
});

describe("nodesWithinDepth", () => {
  // chain: a - b - c - d
  const data: GraphData = {
    nodes: [node("a"), node("b"), node("c"), node("d")],
    links: [{ s: 0, t: 1 }, { s: 1, t: 2 }, { s: 2, t: 3 }],
  };
  const adj = buildAdjacency(data);

  it("depth 0 is just the root", () => {
    expect([...nodesWithinDepth("a", adj, 0)]).toEqual(["a"]);
  });

  it("depth 1 includes immediate neighbors", () => {
    expect([...nodesWithinDepth("a", adj, 1)].sort()).toEqual(["a", "b"]);
  });

  it("depth 2 reaches two hops", () => {
    expect([...nodesWithinDepth("a", adj, 2)].sort()).toEqual(["a", "b", "c"]);
  });

  it("depth beyond diameter returns the whole connected component", () => {
    expect([...nodesWithinDepth("a", adj, 10).values()].sort()).toEqual(["a", "b", "c", "d"]);
  });

  it("unknown root yields an empty set", () => {
    expect(nodesWithinDepth("zzz", adj, 3).size).toBe(0);
  });
});

describe("linkEndId", () => {
  it("returns the id from a bare string endpoint", () => {
    expect(linkEndId("a")).toBe("a");
  });
  it("returns the id from a resolved node object endpoint", () => {
    expect(linkEndId({ id: "b", title: "B" })).toBe("b");
  });
});
