// Editing the mind map means editing the note's Markdown. The read-only map is
// parsed from rendered HTML (lib/outline), which has no source positions; to
// ADD / RENAME / DELETE nodes we instead parse the RAW note body and track each
// node's source line, then splice the text and save the whole file through the
// existing conflict-safe save endpoint. Frontmatter is preserved byte-for-byte.
//
// The parse mirrors lib/outline + the Go internal/mindmap parser (strip a
// duplicate title-H1, headings nest by level, lists by indent, fenced code
// skipped) so the tree — and thus the path ids the map renders — line up. Every
// function here is pure and unit-tested; the component only wires them up.

import { maxOutlineNodes, type OutlineNode, type ParsedOutline } from "./outline";

// A source-aware outline node: an OutlineNode plus the 0-based index of its own
// line within the body (root has none).
export interface SrcNode extends OutlineNode {
  srcLine?: number;
  children: SrcNode[];
}

export interface SrcOutline extends ParsedOutline {
  root: SrcNode;
}

// splitFrontmatter separates a leading YAML frontmatter block (--- … ---) from
// the body, so edits never disturb it. Returns the prefix (incl. trailing
// newline) and the body.
export function splitFrontmatter(file: string): { prefix: string; body: string } {
  if (!file.startsWith("---\n") && !file.startsWith("---\r\n")) return { prefix: "", body: file };
  // Find the closing fence: a line that is exactly "---".
  const lines = file.split("\n");
  for (let i = 1; i < lines.length; i++) {
    if (lines[i]!.trim() === "---") {
      const prefix = lines.slice(0, i + 1).join("\n") + "\n";
      const body = lines.slice(i + 1).join("\n");
      return { prefix, body: body.replace(/^\n+/, "") };
    }
  }
  return { prefix: "", body: file };
}

const headingRe = /^(#{1,6})\s+(.*?)\s*#*\s*$/;
const listRe = /^(\s*)(?:[-*+]|\d+[.)])\s+(.+?)\s*$/;

function fenceMarker(trimmed: string): string {
  if (trimmed.startsWith("```")) return "```";
  if (trimmed.startsWith("~~~")) return "~~~";
  return "";
}
function indentWidth(s: string): number {
  let w = 0;
  for (const ch of s) w += ch === "\t" ? 2 : 1;
  return w;
}

// stripTitleH1Line returns the body line index the outline starts at: if the
// first non-blank line is "# <title>" duplicating the note title, the outline
// begins after it (that line stays in the body but is not a node). Mirrors the
// server's stripTitleH1 so paths match the rendered map.
function titleH1Skip(lines: string[], title: string): number {
  let i = 0;
  while (i < lines.length && lines[i]!.trim() === "") i++;
  if (i >= lines.length) return 0;
  const h = lines[i]!.trim();
  if (!h.startsWith("# ")) return 0;
  if (h.slice(1).trim().toLowerCase() !== title.trim().toLowerCase()) return 0;
  return i + 1;
}

// parseOutlineSource parses a raw note body into a source-tracked tree rooted at
// the title. Same nesting model as lib/outline, but each node carries srcLine.
export function parseOutlineSource(body: string, title: string): SrcOutline {
  const root: SrcNode = { id: "root", text: title || "(untitled)", kind: "root", level: 0, children: [] };
  const out: SrcOutline = { root, count: 1, truncated: false };
  const lines = body.split("\n");
  const start = titleH1Skip(lines, title);

  const hstack: Array<{ node: SrcNode; level: number }> = [{ node: root, level: 0 }];
  const lstack: Array<{ node: SrcNode; indent: number }> = [];
  let inFence = false;
  let fence = "";

  const add = (parent: SrcNode, child: Omit<SrcNode, "id" | "children">): SrcNode | null => {
    if (out.count >= maxOutlineNodes) {
      out.truncated = true;
      return null;
    }
    const node: SrcNode = { ...child, id: `${parent.id}.${parent.children.length}`, children: [] };
    parent.children.push(node);
    out.count++;
    return node;
  };

  for (let i = start; i < lines.length; i++) {
    const raw = lines[i]!;
    const trimmed = raw.trim();
    const fm = fenceMarker(trimmed);
    if (fm) {
      if (!inFence) {
        inFence = true;
        fence = fm;
      } else if (trimmed.startsWith(fence)) {
        inFence = false;
      }
      continue;
    }
    // A blank line does NOT end a list run — CommonMark loose lists keep nesting
    // across blank lines, and the HTML parser (goldmark's nested <ul>) agrees. We
    // only reset the list context on a heading or a genuine prose paragraph.
    if (inFence || trimmed === "") continue;

    const pushHeading = (level: number, text: string, srcLine: number) => {
      while (hstack.length > 1 && hstack[hstack.length - 1]!.level >= level) hstack.pop();
      const node = add(hstack[hstack.length - 1]!.node, { text: text.trim(), kind: "heading", level, srcLine });
      if (node) hstack.push({ node, level });
      lstack.length = 0;
    };

    const hm = headingRe.exec(trimmed);
    if (hm) {
      pushHeading(hm[1]!.length, hm[2]!, i);
      continue;
    }

    const lm = listRe.exec(raw);
    if (lm) {
      const indent = indentWidth(lm[1]!);
      while (lstack.length > 0 && lstack[lstack.length - 1]!.indent >= indent) lstack.pop();
      const parent = lstack.length === 0 ? hstack[hstack.length - 1]!.node : lstack[lstack.length - 1]!.node;
      const node = add(parent, { text: lm[2]!.trim(), kind: "item", level: 0, srcLine: i });
      if (node) lstack.push({ node, indent });
      continue;
    }

    // Setext heading: a prose line UNDERLINED by === (H1) or --- (H2). Matches
    // goldmark/CommonMark so the edit map doesn't reshape vs the rendered map.
    const under = setextLevel(lines[i + 1]);
    if (under && lstack.length === 0) {
      pushHeading(under, trimmed, i);
      i++; // consume the underline line
      continue;
    }

    // A genuine prose paragraph ends the current list run.
    lstack.length = 0;
  }
  return out;
}

// setextLevel returns 1 for an "===" underline, 2 for "---", else 0. A setext
// underline is a line of only = or only - (no other content).
function setextLevel(line: string | undefined): number {
  if (line == null) return 0;
  const t = line.trim();
  if (/^=+$/.test(t)) return 1;
  if (/^-+$/.test(t)) return 2;
  return 0;
}

// --- edit operations -------------------------------------------------------
// Each takes the WHOLE file text (frontmatter + body) and a target node (from
// parseOutlineSource of that same file's body) and returns the new file text.

// flatten returns every non-root node in document (pre-order) order.
function flatten(root: SrcNode): SrcNode[] {
  const out: SrcNode[] = [];
  const walk = (n: SrcNode) => {
    for (const c of n.children) {
      out.push(c);
      walk(c);
    }
  };
  walk(root);
  return out;
}

// subtreeEnd returns the body line index (exclusive) where a node's subtree
// ends: the line of the next node that is not its descendant, else EOF. Trailing
// prose under the node is swallowed into the subtree (so delete removes it too).
function subtreeEnd(node: SrcNode, flat: SrcNode[], totalLines: number): number {
  const idx = flat.indexOf(node);
  for (let i = idx + 1; i < flat.length; i++) {
    const other = flat[i]!;
    if (!other.id.startsWith(node.id + ".")) return other.srcLine!;
  }
  return totalLines;
}

// prefixOf returns the marker prefix of a source line (leading ws + "#…"/bullet
// + the single following space), used to build siblings/children in kind.
function linePrefix(line: string): { kind: "heading" | "list"; indent: string; hashes: string; marker: string } {
  const h = /^(#{1,6})(\s+)/.exec(line);
  if (h) return { kind: "heading", indent: "", hashes: h[1]!, marker: "" };
  const l = /^(\s*)([-*+]|\d+[.)])(\s+)/.exec(line);
  if (l) return { kind: "list", indent: l[1]!, hashes: "", marker: l[2]! };
  return { kind: "list", indent: "", hashes: "", marker: "-" };
}

// oneLine strips newlines from user-entered label text so a node stays one line.
function oneLine(text: string): string {
  return text.replace(/[\r\n]+/g, " ").trim();
}

function withBody(file: string, transform: (lines: string[]) => string[]): string {
  const { prefix, body } = splitFrontmatter(file);
  const lines = body.split("\n");
  // Drop a single trailing empty line (from a final newline) so splicing is
  // predictable; the save endpoint re-adds the terminal newline.
  const hadTrailing = lines.length > 0 && lines[lines.length - 1] === "";
  if (hadTrailing) lines.pop();
  const next = transform(lines);
  return prefix + next.join("\n") + "\n";
}

// childLine builds the source line for a new child of `node`. Headings use the
// node's stored level (so setext headings — whose source line carries no marker
// — still nest correctly); list items read their own indent/marker.
function childLine(node: SrcNode, bodyLines: string[], text: string): string {
  if (node.kind === "root") return `## ${text}`;
  if (node.kind === "heading") {
    return node.level < 6 ? `${"#".repeat(node.level + 1)} ${text}` : `- ${text}`;
  }
  const p = linePrefix(bodyLines[node.srcLine!] ?? "");
  return `${p.indent}  ${p.marker} ${text}`;
}

// siblingLine builds a source line matching `node`'s own kind/level.
function siblingLine(node: SrcNode, bodyLines: string[], text: string): string {
  if (node.kind === "heading") return `${"#".repeat(node.level)} ${text}`;
  const p = linePrefix(bodyLines[node.srcLine!] ?? "");
  return `${p.indent}${p.marker} ${text}`;
}

export function addChild(file: string, node: SrcNode, text: string, flat: SrcNode[]): string {
  const label = oneLine(text);
  return withBody(file, (lines) => {
    const at = node.kind === "root" ? lines.length : subtreeEnd(node, flat, lines.length);
    lines.splice(at, 0, childLine(node, lines, label));
    return lines;
  });
}

export function addSibling(file: string, node: SrcNode, text: string, flat: SrcNode[]): string {
  const label = oneLine(text);
  return withBody(file, (lines) => {
    const at = subtreeEnd(node, flat, lines.length);
    lines.splice(at, 0, siblingLine(node, lines, label));
    return lines;
  });
}

export function renameNode(file: string, node: SrcNode, text: string): string {
  if (node.srcLine == null) return file;
  const label = oneLine(text);
  return withBody(file, (lines) => {
    const src = lines[node.srcLine!] ?? "";
    const m = /^(\s*(?:#{1,6}|[-*+]|\d+[.)])\s+)/.exec(src);
    lines[node.srcLine!] = (m ? m[1] : "") + label;
    return lines;
  });
}

export function deleteNode(file: string, node: SrcNode, flat: SrcNode[]): string {
  if (node.srcLine == null) return file;
  return withBody(file, (lines) => {
    const end = subtreeEnd(node, flat, lines.length);
    lines.splice(node.srcLine!, end - node.srcLine!);
    return lines;
  });
}

// deletePreview reports what a delete would remove: how many mapped nodes (the
// node + its descendants) and how many extra *prose* lines the map never showed
// (paragraphs/quotes/tables/code under a heading) — so the UI can warn before a
// delete silently eats unseen text and always offer an undo.
export function deletePreview(
  file: string,
  node: SrcNode,
  flat: SrcNode[],
): { nodeCount: number; proseLines: number } {
  if (node.srcLine == null) return { nodeCount: 0, proseLines: 0 };
  const { body } = splitFrontmatter(file);
  const lines = body.split("\n");
  const end = subtreeEnd(node, flat, lines.length);
  // Node lines within the range (the node itself + its descendants).
  const nodeLines = new Set<number>([node.srcLine]);
  for (const d of flat) if (d.id.startsWith(node.id + ".") && d.srcLine != null) nodeLines.add(d.srcLine);
  let proseLines = 0;
  let inFence = false;
  let fence = "";
  for (let i = node.srcLine; i < end && i < lines.length; i++) {
    const t = lines[i]!.trim();
    const fm = fenceMarker(t);
    if (fm) {
      if (!inFence) ((inFence = true), (fence = fm));
      else if (t.startsWith(fence)) inFence = false;
      proseLines++; // code fences count as prose being removed
      continue;
    }
    if (inFence) {
      proseLines++;
      continue;
    }
    if (t === "" || nodeLines.has(i) || setextLevel(lines[i + 1])) continue;
    if (setextLevel(t)) continue; // a setext underline belongs to its heading
    proseLines++;
  }
  return { nodeCount: nodeLines.size, proseLines };
}

// isAncestor reports whether `maybe` is `node` or one of its descendants (by path
// id), so a reparent can refuse to drop a node into its own subtree.
export function isAncestor(node: SrcNode, maybe: SrcNode): boolean {
  return maybe.id === node.id || maybe.id.startsWith(node.id + ".");
}

// moveNode reparents `node`'s subtree under `newParent` by relocating its source
// lines and shifting outline levels by a uniform delta — headings by heading
// level, list items by indent — so prose/code inside the subtree moves intact and
// only the nesting changes. Invalid drops (into itself, or a heading under a list
// item) return the file unchanged.
export function moveNode(file: string, node: SrcNode, newParent: SrcNode, flat: SrcNode[]): string {
  if (node.srcLine == null || isAncestor(node, newParent)) return file;
  const { prefix, body } = splitFrontmatter(file);
  const lines = body.split("\n");
  if (lines.length && lines[lines.length - 1] === "") lines.pop();

  const end = subtreeEnd(node, flat, lines.length);
  const block = lines.slice(node.srcLine, end);

  // Target base: children of the new parent sit at heading level (parentLevel+1)
  // or indent (parentIndent+2); under the root/a heading, moved bullets go to
  // indent 0.
  let headingDelta = 0;
  let indentDelta = 0;
  if (node.kind === "heading") {
    if (newParent.kind === "item") return file; // a heading under a bullet is invalid
    const base = newParent.kind === "root" ? 0 : newParent.level;
    headingDelta = base + 1 - node.level;
    // Refuse a move that would push any heading past H6 — clamping would collapse
    // distinct levels into a flat run of H6s (silent hierarchy loss).
    if (headingDelta > 0) {
      let maxLevel = 0;
      for (const line of block) {
        const h = /^(#{1,6})\s/.exec(line);
        if (h) maxLevel = Math.max(maxLevel, h[1]!.length);
      }
      if (maxLevel + headingDelta > 6) return file;
    }
  } else {
    const curIndent = indentWidth(/^(\s*)/.exec(lines[node.srcLine] ?? "")?.[1] ?? "");
    const target =
      newParent.kind === "item"
        ? indentWidth(/^(\s*)/.exec(lines[newParent.srcLine!] ?? "")?.[1] ?? "") + 2
        : 0;
    indentDelta = target - curIndent;
  }

  const shifted = block.map((line) => {
    const h = /^(#{1,6})(\s+)(.*)$/.exec(line);
    if (h && headingDelta) {
      const lvl = Math.min(6, Math.max(1, h[1]!.length + headingDelta));
      return "#".repeat(lvl) + h[2] + h[3];
    }
    const l = /^(\s*)([-*+]|\d+[.)])(\s+.*)$/.exec(line);
    if (l && indentDelta) {
      const indent = Math.max(0, indentWidth(l[1]!) + indentDelta);
      return " ".repeat(indent) + l[2] + l[3];
    }
    return line;
  });

  // Remove the block, then insert at the new parent's subtree end (recomputed
  // against the shortened array — adjust for a removal that was above it).
  lines.splice(node.srcLine, block.length);
  const flatAfter = flat.filter((n) => !isAncestor(node, n));
  let insertAt =
    newParent.kind === "root"
      ? lines.length
      : subtreeEndAfterRemoval(newParent, flatAfter, lines.length, node.srcLine, block.length);
  insertAt = Math.min(Math.max(insertAt, 0), lines.length);
  lines.splice(insertAt, 0, ...shifted);
  return prefix + lines.join("\n") + "\n";
}

// subtreeEndAfterRemoval is subtreeEnd for the new parent, but with source lines
// at/after `removedAt` shifted up by `removedCount` (the moved block is gone).
function subtreeEndAfterRemoval(
  parent: SrcNode,
  flat: SrcNode[],
  totalLines: number,
  removedAt: number,
  removedCount: number,
): number {
  const shift = (ln: number) => (ln >= removedAt + removedCount ? ln - removedCount : ln);
  const idx = flat.indexOf(parent);
  for (let i = idx + 1; i < flat.length; i++) {
    const other = flat[i]!;
    if (!other.id.startsWith(parent.id + ".")) return shift(other.srcLine!);
  }
  return totalLines;
}

// findByPath locates a node by its stable path id in a freshly parsed tree — the
// component re-parses the current raw body before each edit, then resolves the
// clicked node's id here, so edits never act on a stale tree.
export function findByPath(root: SrcNode, id: string): SrcNode | null {
  if (id === "root") return root;
  let node: SrcNode = root;
  const parts = id.split(".").slice(1); // drop "root"
  for (const p of parts) {
    const i = Number(p);
    if (!node.children[i]) return null;
    node = node.children[i]!;
  }
  return node;
}

export { flatten as flattenSrc };

// --- collapse-state remapping ----------------------------------------------
// Node ids are positional paths, so inserting/removing a sibling shifts the ids
// of later siblings. These keep a collapse Set pointing at the same *nodes*
// across a structural edit (issue: collapse/focus "jumping" after edits).

function reindexUnder(set: ReadonlySet<string>, parentId: string, fromIndex: number, delta: number): Set<string> {
  const prefix = parentId + ".";
  const out = new Set<string>();
  for (const id of set) {
    if (!id.startsWith(prefix)) {
      out.add(id);
      continue;
    }
    const rest = id.slice(prefix.length);
    const dot = rest.indexOf(".");
    const idx = Number(dot === -1 ? rest : rest.slice(0, dot));
    const tail = dot === -1 ? "" : rest.slice(dot);
    out.add(!Number.isNaN(idx) && idx >= fromIndex ? `${parentId}.${idx + delta}${tail}` : id);
  }
  return out;
}

// collapseAfterInsert shifts ids for a sibling inserted at `index` under parent.
export function collapseAfterInsert(set: ReadonlySet<string>, parentId: string, index: number): Set<string> {
  return reindexUnder(set, parentId, index, 1);
}

// collapseAfterDelete drops the removed subtree's ids, then shifts later siblings.
export function collapseAfterDelete(set: ReadonlySet<string>, parentId: string, index: number): Set<string> {
  const gone = `${parentId}.${index}`;
  const kept = new Set<string>();
  for (const id of set) if (id !== gone && !id.startsWith(gone + ".")) kept.add(id);
  return reindexUnder(kept, parentId, index + 1, -1);
}
