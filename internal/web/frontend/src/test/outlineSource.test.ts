import { describe, it, expect } from "vitest";
import {
  splitFrontmatter,
  parseOutlineSource,
  addChild,
  addSibling,
  renameNode,
  deleteNode,
  findByPath,
  flattenSrc,
  type SrcNode,
} from "../lib/outlineSource";

const FM = "---\nid: 01ABC\ntitle: T\n---\n";

function shape(n: SrcNode): string {
  const kids = n.children.map(shape).join(",");
  return kids ? `${n.text}[${kids}]` : n.text;
}

describe("splitFrontmatter", () => {
  it("separates YAML frontmatter from the body", () => {
    const { prefix, body } = splitFrontmatter(FM + "## A\n- x\n");
    expect(prefix).toBe(FM);
    expect(body).toBe("## A\n- x\n");
  });
  it("returns empty prefix when there is none", () => {
    const { prefix, body } = splitFrontmatter("## A\n");
    expect(prefix).toBe("");
    expect(body).toBe("## A\n");
  });
});

describe("parseOutlineSource", () => {
  it("tracks each node's source line and nests correctly", () => {
    const body = "## A\n- one\n  - two\n## B\n";
    const { root } = parseOutlineSource(body, "T");
    expect(shape(root)).toBe("T[A[one[two]],B]");
    const [a, one, two, b] = flattenSrc(root);
    expect([a!.srcLine, one!.srcLine, two!.srcLine, b!.srcLine]).toEqual([0, 1, 2, 3]);
  });

  it("skips a duplicate leading title-H1 (line stays, not a node)", () => {
    const body = "# My Note\n\n## A\n";
    const { root } = parseOutlineSource(body, "My Note");
    expect(shape(root)).toBe("My Note[A]");
    expect(root.children[0]!.srcLine).toBe(2); // "## A" is on line index 2
  });
});

describe("edit ops", () => {
  const file = FM + "## Goals\n- Ship\n## Risks\n";
  const parse = (f: string) => parseOutlineSource(splitFrontmatter(f).body, "T");

  it("addChild under a heading inserts a deeper heading, preserving frontmatter", () => {
    const { root } = parse(file);
    const goals = findByPath(root, "root.0")!; // ## Goals
    const out = addChild(file, goals, "New goal", flattenSrc(root));
    expect(out.startsWith(FM)).toBe(true); // frontmatter intact
    expect(out).toContain("### New goal");
    // Inserted after Goals' subtree (after "- Ship"), before "## Risks".
    expect(out.indexOf("### New goal")).toBeLessThan(out.indexOf("## Risks"));
    expect(out.indexOf("- Ship")).toBeLessThan(out.indexOf("### New goal"));
  });

  it("addChild under a list item nests a bullet", () => {
    const { root } = parse(file);
    const ship = findByPath(root, "root.0.0")!; // - Ship
    const out = addChild(file, ship, "sub", flattenSrc(root));
    expect(out).toContain("  - sub");
  });

  it("addChild of the root adds a top-level H2 at the end", () => {
    const { root } = parse(file);
    const out = addChild(file, root, "Appendix", flattenSrc(root));
    expect(out).toContain("## Appendix");
    expect(out.trimEnd().endsWith("## Appendix")).toBe(true);
  });

  it("addSibling matches the target's level", () => {
    const { root } = parse(file);
    const goals = findByPath(root, "root.0")!;
    const out = addSibling(file, goals, "Timeline", flattenSrc(root));
    expect(out).toContain("## Timeline");
    // Sibling lands right after Goals' subtree.
    expect(out.indexOf("## Timeline")).toBeLessThan(out.indexOf("## Risks"));
  });

  it("renameNode preserves the marker", () => {
    const { root } = parse(file);
    const ship = findByPath(root, "root.0.0")!;
    const out = renameNode(file, ship, "Ship v2");
    expect(out).toContain("- Ship v2");
    expect(out).not.toContain("- Ship\n");
  });

  it("deleteNode removes the node and its subtree", () => {
    const withKid = FM + "## Goals\n- Ship\n  - Auth\n## Risks\n";
    const { root } = parse(withKid);
    const goals = findByPath(root, "root.0")!;
    const out = deleteNode(withKid, goals, flattenSrc(root));
    expect(out).not.toContain("Goals");
    expect(out).not.toContain("Ship");
    expect(out).not.toContain("Auth");
    expect(out).toContain("## Risks"); // sibling untouched
    expect(out.startsWith(FM)).toBe(true);
  });

  it("past H6, a child drops to a bullet", () => {
    const deep = FM + "###### Deep\n";
    const { root } = parse(deep);
    const d = findByPath(root, "root.0")!;
    const out = addChild(deep, d, "leaf", flattenSrc(root));
    expect(out).toContain("- leaf");
  });

  it("collapses newlines in entered text to keep one node per line", () => {
    const { root } = parse(file);
    const out = addChild(file, findByPath(root, "root.0")!, "a\nb", flattenSrc(root));
    expect(out).toContain("### a b");
  });

  it("round-trips: edit output re-parses into the expected tree (the app's cycle)", () => {
    // add a child under Goals, then a sibling of Goals, then rename Ship.
    let f = file;
    let t = parse(f);
    f = addChild(f, findByPath(t.root, "root.0")!, "New goal", flattenSrc(t.root));
    t = parse(f);
    f = addSibling(f, findByPath(t.root, "root.0")!, "Timeline", flattenSrc(t.root));
    t = parse(f);
    f = renameNode(f, findByPath(t.root, "root.0.0")!, "Ship it"); // Ship → Ship it
    t = parse(f);
    expect(shape(t.root)).toBe("T[Goals[Ship it,New goal],Timeline,Risks]");
    expect(f.startsWith(FM)).toBe(true); // frontmatter survived every edit
  });
});
