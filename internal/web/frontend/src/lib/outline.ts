// The intra-note "mind map" is the note's own document outline — its headings
// and nested list items — turned into a radial tree (the markmap model: an H1
// is the trunk, H2…H6 are branches, and nested bullets are the leaves). We build
// it from the *rendered* body HTML rather than the raw Markdown for two reasons:
// goldmark has already given every heading a stable `id` (so a map node can
// scroll the prose to its heading with no slug-guessing), and the leading
// title-H1 has already been stripped server-side (stripTitleH1), so the tree
// lines up 1:1 with what the reader sees. Parsing a string into a tree is a pure,
// unit-tested seam (jsdom gives us DOMParser under vitest) — the SVG renderer is
// the only part that needs a live canvas.

export type OutlineKind = "root" | "heading" | "item";

export interface OutlineNode {
  id: string; // stable path key ("root", "root.0", "root.0.1") for collapse state + keyed each
  text: string; // display label
  kind: OutlineKind;
  level: number; // heading tag level (1–6) for headings; 0 for root/items (styling hint only)
  anchor?: string; // heading DOM id, for "scroll the prose to here" (headings only)
  children: OutlineNode[];
}

export interface ParsedOutline {
  root: OutlineNode;
  count: number; // total nodes incl. root
  truncated: boolean; // hit the node cap — some detail was dropped
}

// maxOutlineNodes bounds a single map so a pathological note (thousands of list
// items) can't wedge the layout/render. Mirrors the graph's maxGraphNodes cap.
export const maxOutlineNodes = 400;

// ownText returns an element's text with any nested list content removed, so a
// list item that contains a sub-list contributes only its own label (the
// sub-list becomes child nodes instead of being duplicated into the parent).
function ownText(el: Element): string {
  const clone = el.cloneNode(true) as Element;
  clone.querySelectorAll("ul, ol").forEach((n) => n.remove());
  return (clone.textContent ?? "").replace(/\s+/g, " ").trim();
}

// parseOutline turns rendered note HTML into a tree rooted at the note title.
// Headings nest by their level (a well-formed or malformed order both collapse
// sensibly via the level stack); a list encountered under a heading (or before
// the first heading, under the root) contributes its items, recursively. Plain
// paragraphs and other blocks are intentionally ignored — a mind map is the
// skeleton, not the prose.
export function parseOutline(html: string, title: string): ParsedOutline {
  const root: OutlineNode = { id: "root", text: title || "(untitled)", kind: "root", level: 0, children: [] };
  const out: ParsedOutline = { root, count: 1, truncated: false };
  if (!html || typeof DOMParser === "undefined") return out;

  const doc = new DOMParser().parseFromString(html, "text/html");
  const body = doc.body;
  if (!body) return out;

  // Stack of open heading contexts, deepest last. Each entry pairs a node with
  // the heading level it was opened at (root sits at level 0 and never pops).
  const stack: Array<{ node: OutlineNode; level: number }> = [{ node: root, level: 0 }];
  const top = () => stack[stack.length - 1]!.node; // root anchors the stack; never empty

  // add appends child to parent, assigns its stable path id, and enforces the cap.
  const add = (parent: OutlineNode, child: Omit<OutlineNode, "id" | "children">): OutlineNode | null => {
    if (out.count >= maxOutlineNodes) {
      out.truncated = true;
      return null;
    }
    const node: OutlineNode = { ...child, id: `${parent.id}.${parent.children.length}`, children: [] };
    parent.children.push(node);
    out.count++;
    return node;
  };

  // addList walks a <ul>/<ol>, attaching each <li> under parent and recursing
  // into any nested list the item holds.
  const addList = (list: Element, parent: OutlineNode): void => {
    for (const li of Array.from(list.children)) {
      if (li.tagName !== "LI") continue;
      const node = add(parent, { text: ownText(li), kind: "item", level: 0 });
      if (!node) return;
      for (const sub of Array.from(li.children)) {
        if (sub.tagName === "UL" || sub.tagName === "OL") addList(sub, node);
      }
    }
  };

  // Walk the body's top-level blocks in document order. Headings restructure the
  // stack; top-level lists attach to the current heading context.
  for (const el of Array.from(body.children)) {
    const tag = el.tagName;
    if (/^H[1-6]$/.test(tag)) {
      const level = Number(tag[1]);
      while (stack.length > 1 && stack[stack.length - 1]!.level >= level) stack.pop();
      const node = add(top(), {
        text: (el.textContent ?? "").replace(/\s+/g, " ").trim(),
        kind: "heading",
        level,
        anchor: el.id || undefined,
      });
      if (node) stack.push({ node, level });
    } else if (tag === "UL" || tag === "OL") {
      addList(el, top());
    }
  }

  return out;
}

// subtreeCount counts a node's descendants (its whole subtree, collapse aside) —
// used to label a collapsed node with how many nodes it's hiding.
export function subtreeCount(node: OutlineNode): number {
  let n = 0;
  for (const c of node.children) n += 1 + subtreeCount(c);
  return n;
}

// maxDepth returns the deepest level in the tree (root = 0), so the
// "collapse to level" control knows how many rings exist.
export function maxDepth(root: OutlineNode): number {
  let d = 0;
  const walk = (x: OutlineNode, depth: number) => {
    d = Math.max(d, depth);
    for (const c of x.children) walk(c, depth + 1);
  };
  walk(root, 0);
  return d;
}

// collapseToDepth collects the ids of every node at `depth` or deeper that has
// children — collapsing this set yields "show me only the first N rings".
export function collapseToDepth(root: OutlineNode, depth: number): Set<string> {
  const ids = new Set<string>();
  const walk = (x: OutlineNode, d: number) => {
    if (d >= depth && x.children.length) ids.add(x.id);
    if (d < depth) for (const c of x.children) walk(c, d + 1);
  };
  walk(root, 0);
  return ids;
}
