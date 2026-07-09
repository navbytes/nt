import { describe, it, expect } from "vitest";
import {
  parseOutline,
  maxDepth,
  collapseToDepth,
  subtreeCount,
  maxOutlineNodes,
  type OutlineNode,
} from "../lib/outline";

// Shorthand: flatten a tree to "text@kind" for compact assertions.
function shape(n: OutlineNode): string {
  const kids = n.children.map(shape).join(",");
  return kids ? `${n.text}[${kids}]` : n.text;
}

describe("parseOutline", () => {
  it("roots the tree at the note title", () => {
    const { root } = parseOutline("<p>body</p>", "My Note");
    expect(root.text).toBe("My Note");
    expect(root.kind).toBe("root");
    expect(root.children).toHaveLength(0);
  });

  it("nests headings by level", () => {
    const html = "<h2>A</h2><h3>A1</h3><h3>A2</h3><h2>B</h2>";
    const { root } = parseOutline(html, "T");
    expect(shape(root)).toBe("T[A[A1,A2],B]");
  });

  it("pops back up when a shallower heading follows a deeper one", () => {
    const html = "<h2>A</h2><h4>deep</h4><h2>B</h2>";
    const { root } = parseOutline(html, "T");
    expect(shape(root)).toBe("T[A[deep],B]");
  });

  it("attaches a list under the current heading and nests sub-lists", () => {
    const html = "<h2>A</h2><ul><li>one<ul><li>one-a</li></ul></li><li>two</li></ul>";
    const { root } = parseOutline(html, "T");
    expect(shape(root)).toBe("T[A[one[one-a],two]]");
  });

  it("attaches a pre-heading list directly under the root", () => {
    const html = "<ul><li>intro</li></ul><h2>A</h2>";
    const { root } = parseOutline(html, "T");
    expect(shape(root)).toBe("T[intro,A]");
  });

  it("keeps the heading id as an anchor and ignores plain paragraphs", () => {
    const html = '<h2 id="sec-a">A</h2><p>prose we ignore</p>';
    const { root } = parseOutline(html, "T");
    expect(root.children).toHaveLength(1);
    expect(root.children[0]!.anchor).toBe("sec-a");
    expect(root.children[0]!.level).toBe(2);
  });

  it("gives every node a stable path id", () => {
    const html = "<h2>A</h2><ul><li>x</li></ul>";
    const { root } = parseOutline(html, "T");
    expect(root.id).toBe("root");
    expect(root.children[0]!.id).toBe("root.0");
    expect(root.children[0]!.children[0]!.id).toBe("root.0.0");
  });

  it("caps the node count and flags truncation", () => {
    const items = Array.from({ length: maxOutlineNodes + 50 }, (_, i) => `<li>i${i}</li>`).join("");
    const { count, truncated } = parseOutline(`<ul>${items}</ul>`, "T");
    expect(truncated).toBe(true);
    expect(count).toBeLessThanOrEqual(maxOutlineNodes);
  });

  it("is empty-safe without a DOM parser / empty html", () => {
    const { root, count } = parseOutline("", "T");
    expect(count).toBe(1);
    expect(root.children).toHaveLength(0);
  });
});

describe("tree helpers", () => {
  const { root } = parseOutline("<h2>A</h2><h3>A1</h3><h2>B</h2>", "T");

  it("maxDepth counts rings from the root", () => {
    expect(maxDepth(root)).toBe(2); // T(0) → A(1) → A1(2)
  });

  it("subtreeCount counts descendants", () => {
    expect(subtreeCount(root)).toBe(3); // A, A1, B
    expect(subtreeCount(root.children[0]!)).toBe(1); // A → A1
  });

  it("collapseToDepth targets nodes at/under the depth with children", () => {
    // depth 1 and deeper, with children → only A (root.0) has a child.
    const ids = collapseToDepth(root, 1);
    expect([...ids]).toEqual(["root.0"]);
  });
});
