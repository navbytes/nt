import { describe, it, expect } from "vitest";
import { stepIndex, parentIndex, firstChildIndex, type TreeNavItem } from "../lib/treenav";

// A small visible tree (already flattened in DOM order — collapsed folders'
// children simply aren't present):
//   0  Folder A         level 1   (expanded)
//   1    note a1        level 2
//   2    Folder B       level 2   (expanded)
//   3      note b1      level 3
//   4  note top         level 1
const tree: TreeNavItem[] = [
  { level: 1 },
  { level: 2 },
  { level: 2 },
  { level: 3 },
  { level: 1 },
];

describe("stepIndex (ArrowDown/Up, Home/End)", () => {
  it("from nothing focused, down lands on first, up on last", () => {
    expect(stepIndex(tree.length, -1, 1)).toBe(0);
    expect(stepIndex(tree.length, -1, -1)).toBe(tree.length - 1);
  });
  it("moves to the adjacent visible node", () => {
    expect(stepIndex(tree.length, 1, 1)).toBe(2);
    expect(stepIndex(tree.length, 2, -1)).toBe(1);
  });
  it("clamps at the ends (no wrap)", () => {
    expect(stepIndex(tree.length, tree.length - 1, 1)).toBe(tree.length - 1);
    expect(stepIndex(tree.length, 0, -1)).toBe(0);
  });
  it("returns -1 for an empty tree", () => {
    expect(stepIndex(0, -1, 1)).toBe(-1);
  });
});

describe("parentIndex (ArrowLeft → parent)", () => {
  it("finds the nearest preceding shallower item", () => {
    expect(parentIndex(tree, 3)).toBe(2); // b1 → Folder B
    expect(parentIndex(tree, 2)).toBe(0); // Folder B → Folder A
    expect(parentIndex(tree, 1)).toBe(0); // a1 → Folder A
  });
  it("returns -1 for a top-level item", () => {
    expect(parentIndex(tree, 0)).toBe(-1);
    expect(parentIndex(tree, 4)).toBe(-1);
  });
});

describe("firstChildIndex (ArrowRight on an expanded folder)", () => {
  it("returns the next item when it is one level deeper", () => {
    expect(firstChildIndex(tree, 0)).toBe(1); // Folder A → a1
    expect(firstChildIndex(tree, 2)).toBe(3); // Folder B → b1
  });
  it("returns -1 when the next item is not a child (collapsed / a note / last)", () => {
    expect(firstChildIndex(tree, 1)).toBe(-1); // note a1 → next is a sibling
    expect(firstChildIndex(tree, 3)).toBe(-1); // b1 → next is a level-1 item
    expect(firstChildIndex(tree, 4)).toBe(-1); // last item, no next
  });
});
