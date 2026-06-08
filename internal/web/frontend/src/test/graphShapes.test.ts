import { describe, it, expect } from "vitest";
import { nodeShape, shapeValue, shapeLegendEntries, glyphPoints } from "../lib/graphShapes";
import type { FGNode } from "../lib/graph";

function node(id: string, source = "cli", folder = "", tags: string[] = []): FGNode {
  return { id, title: id.toUpperCase(), url: `/n/${id}`, folder, source, tags, deg: 0 };
}

describe("nodeShape", () => {
  it("returns circle when shapeBy is none", () => {
    expect(nodeShape(node("a", "claude"), "none")).toBe("circle");
  });

  it("returns circle for an empty value in the chosen dimension", () => {
    expect(nodeShape(node("a", ""), "source")).toBe("circle");
  });

  it("is stable per value and distinct across values", () => {
    const a1 = nodeShape(node("a", "cli"), "source");
    const a2 = nodeShape(node("b", "cli"), "source");
    const c = nodeShape(node("c", "claude"), "source");
    expect(a1).toBe(a2); // same source → same shape
    expect(a1).not.toBe(c); // different source → different shape
  });
});

describe("shapeValue", () => {
  it("reads the first tag when shaping by tag", () => {
    expect(shapeValue(node("a", "cli", "", ["x", "y"]), "tag")).toBe("x");
  });
});

describe("shapeLegendEntries", () => {
  it("lists one entry per distinct value, sorted, with a shape", () => {
    const nodes = [node("a", "cli"), node("b", "claude"), node("c", "cli")];
    const entries = shapeLegendEntries(nodes, "source");
    expect(entries.map((e) => e.value)).toEqual(["claude", "cli"]);
    expect(entries.every((e) => typeof e.shape === "string")).toBe(true);
  });

  it("is empty when shapeBy is none", () => {
    expect(shapeLegendEntries([node("a")], "none")).toHaveLength(0);
  });
});

describe("glyphPoints", () => {
  it("produces polygon points for non-circle shapes", () => {
    for (const k of ["diamond", "square", "triangle", "hexagon", "star"] as const) {
      expect(glyphPoints(k).split(" ").length).toBeGreaterThanOrEqual(3);
    }
  });
});
