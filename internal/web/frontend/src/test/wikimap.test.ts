import { describe, it, expect } from "vitest";
import { wikiTree } from "../lib/wikimap";
import type { GraphData } from "../lib/api-types";

function gnode(id: string, kind = "note") {
  return { id, kind, title: id.toUpperCase(), url: `/n/${id}`, folder: "", source: "cli", tags: [], deg: 0 };
}

// a—b, a—c, b—d ; plus a task edge that must be ignored.
function data(): GraphData {
  return {
    nodes: [gnode("a"), gnode("b"), gnode("c"), gnode("d"), gnode("t1", "task")],
    links: [
      { s: 0, t: 1, kind: "wikilink" },
      { s: 0, t: 2, kind: "wikilink" },
      { s: 1, t: 3, kind: "wikilink" },
      { s: 0, t: 4, kind: "task" }, // a—t1 task edge: excluded
    ],
  };
}

function shape(n: { text: string; children: any[] }): string {
  const kids = n.children.map(shape).join(",");
  return kids ? `${n.text}[${kids}]` : n.text;
}

describe("wikiTree", () => {
  it("roots on the given note and radiates along wikilinks", () => {
    const { root } = wikiTree(data(), "a", 3);
    expect(shape(root)).toBe("A[B[D],C]"); // b/c sorted by title; d under b
    expect(root.kind).toBe("root");
    expect(root.anchor).toBe("/n/a");
  });

  it("carries each note's url as the anchor for navigation", () => {
    const { root } = wikiTree(data(), "a", 3);
    expect(root.children[0]!.anchor).toBe("/n/b");
  });

  it("excludes task nodes and non-wikilink edges", () => {
    const { root, count } = wikiTree(data(), "a", 3);
    const texts = new Set<string>();
    const walk = (n: { text: string; children: any[] }) => {
      texts.add(n.text);
      n.children.forEach(walk);
    };
    walk(root);
    expect(texts.has("T1")).toBe(false);
    expect(count).toBe(4); // a, b, c, d
  });

  it("respects the depth limit", () => {
    const { root } = wikiTree(data(), "a", 1); // only immediate neighbors
    expect(shape(root)).toBe("A[B,C]"); // d (2 hops) excluded
  });

  it("places each note once at its shortest distance", () => {
    // diamond: a—b, a—c, b—d, c—d. d is 2 hops via either; appears once.
    const diamond: GraphData = {
      nodes: [gnode("a"), gnode("b"), gnode("c"), gnode("d")],
      links: [
        { s: 0, t: 1, kind: "wikilink" },
        { s: 0, t: 2, kind: "wikilink" },
        { s: 1, t: 3, kind: "wikilink" },
        { s: 2, t: 3, kind: "wikilink" },
      ],
    };
    const { root, count } = wikiTree(diamond, "a", 5);
    expect(count).toBe(4); // no duplicate d
    expect(shape(root)).toBe("A[B[D],C]"); // d attaches to the first visited parent (b)
  });

  it("is safe when the root isn't in the graph", () => {
    const { root, count } = wikiTree(data(), "missing", 3);
    expect(count).toBe(1);
    expect(root.children).toHaveLength(0);
  });
});
