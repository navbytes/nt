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
    if (inFence || trimmed === "") {
      if (trimmed === "") lstack.length = 0;
      continue;
    }

    const hm = headingRe.exec(trimmed);
    if (hm) {
      const level = hm[1]!.length;
      while (hstack.length > 1 && hstack[hstack.length - 1]!.level >= level) hstack.pop();
      const node = add(hstack[hstack.length - 1]!.node, {
        text: hm[2]!.trim(),
        kind: "heading",
        level,
        srcLine: i,
      });
      if (node) hstack.push({ node, level });
      lstack.length = 0;
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

    // Any other prose line ends the current list run.
    lstack.length = 0;
  }
  return out;
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

// childLine builds the source line for a new child of `node`.
function childLine(node: SrcNode, bodyLines: string[], text: string): string {
  if (node.kind === "root") return `## ${text}`;
  const src = bodyLines[node.srcLine!] ?? "";
  const p = linePrefix(src);
  if (p.kind === "heading") {
    if (p.hashes.length < 6) return `${p.hashes}# ${text}`;
    return `- ${text}`; // past H6, drop to a bullet
  }
  return `${p.indent}  ${p.marker} ${text}`;
}

// siblingLine builds a source line matching `node`'s own kind/level.
function siblingLine(node: SrcNode, bodyLines: string[], text: string): string {
  const src = bodyLines[node.srcLine!] ?? "";
  const p = linePrefix(src);
  if (p.kind === "heading") return `${p.hashes} ${text}`;
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
