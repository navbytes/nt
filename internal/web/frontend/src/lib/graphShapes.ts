import type { FGNode } from "./graph";
import type { ShapeBy } from "./graphView.svelte";

// Shape is the graph's second categorical channel (color is the first). It only
// reads for a few categories, so the vocabulary is short and circle is the
// neutral default — assigned first and used for "none"/empty values. Keyed per
// dimension (like graphColors) so each "shape by" choice gets a stable mapping.
export type ShapeKind = "circle" | "diamond" | "square" | "triangle" | "hexagon" | "star";

const SHAPES: ShapeKind[] = ["circle", "diamond", "square", "triangle", "hexagon", "star"];

const assigned = new Map<string, ShapeKind>();

function shapeForKey(key: string): ShapeKind {
  let s = assigned.get(key);
  if (!s) {
    s = SHAPES[assigned.size % SHAPES.length] as ShapeKind;
    assigned.set(key, s);
  }
  return s;
}

// shapeValue is the single value a node is shaped by under the active dimension
// (first tag when shaping by tag; a note can carry many).
export function shapeValue(n: FGNode, shapeBy: ShapeBy): string {
  switch (shapeBy) {
    case "tag":
      return n.tags[0] ?? "";
    case "folder":
      return n.folder;
    case "source":
      return n.source ?? "";
    default:
      return "";
  }
}

export function nodeShape(n: FGNode, shapeBy: ShapeBy): ShapeKind {
  if (shapeBy === "none") return "circle";
  const v = shapeValue(n, shapeBy);
  if (!v) return "circle"; // uncategorised → the neutral shape
  return shapeForKey(shapeBy + ":" + v);
}

export function shapeLegendEntries(
  nodes: FGNode[],
  shapeBy: ShapeBy,
): { value: string; label: string; shape: ShapeKind }[] {
  if (shapeBy === "none") return [];
  const seen = new Set<string>();
  for (const n of nodes) seen.add(shapeValue(n, shapeBy));
  return [...seen]
    .sort((a, b) => a.localeCompare(b))
    .map((value) => ({
      value,
      label: value || "(none)",
      shape: value ? shapeForKey(shapeBy + ":" + value) : "circle",
    }));
}

// tracePath opens a canvas path for `kind` centered at (x,y) with ~radius r; the
// caller fills and/or strokes. Non-circle sizes are tuned to roughly match a
// circle of radius r in visual weight.
export function tracePath(
  ctx: CanvasRenderingContext2D,
  kind: ShapeKind,
  x: number,
  y: number,
  r: number,
): void {
  ctx.beginPath();
  if (kind === "square") {
    const h = r * 0.86;
    ctx.rect(x - h, y - h, 2 * h, 2 * h);
  } else if (kind === "diamond") {
    ctx.moveTo(x, y - r);
    ctx.lineTo(x + r, y);
    ctx.lineTo(x, y + r);
    ctx.lineTo(x - r, y);
    ctx.closePath();
  } else if (kind === "triangle") {
    const a = r * 1.1;
    ctx.moveTo(x, y - a);
    ctx.lineTo(x + a * 0.87, y + a * 0.5);
    ctx.lineTo(x - a * 0.87, y + a * 0.5);
    ctx.closePath();
  } else if (kind === "hexagon" || kind === "star") {
    const steps = kind === "hexagon" ? 6 : 10;
    for (let i = 0; i < steps; i++) {
      const rad = kind === "star" && i % 2 ? r * 0.45 : r;
      const ang = (Math.PI / (steps / 2)) * i - Math.PI / 2;
      const px = x + rad * Math.cos(ang);
      const py = y + rad * Math.sin(ang);
      if (i === 0) ctx.moveTo(px, py);
      else ctx.lineTo(px, py);
    }
    ctx.closePath();
  } else {
    ctx.arc(x, y, r, 0, 2 * Math.PI);
  }
}

// glyphPoints returns SVG polygon points for a 12×12 box (for the legend glyph);
// circle is rendered as <circle> by the caller, so it's not produced here.
export function glyphPoints(kind: ShapeKind): string {
  const c = 6;
  const r = 5;
  if (kind === "square") {
    const h = r * 0.86;
    return `${c - h},${c - h} ${c + h},${c - h} ${c + h},${c + h} ${c - h},${c + h}`;
  }
  if (kind === "diamond") return `${c},${c - r} ${c + r},${c} ${c},${c + r} ${c - r},${c}`;
  if (kind === "triangle") {
    const a = r * 1.1;
    return `${c},${c - a} ${c + a * 0.87},${c + a * 0.5} ${c - a * 0.87},${c + a * 0.5}`;
  }
  const steps = kind === "hexagon" ? 6 : 10;
  const pts: string[] = [];
  for (let i = 0; i < steps; i++) {
    const rad = kind === "star" && i % 2 ? r * 0.45 : r;
    const ang = (Math.PI / (steps / 2)) * i - Math.PI / 2;
    pts.push(`${(c + rad * Math.cos(ang)).toFixed(2)},${(c + rad * Math.sin(ang)).toFixed(2)}`);
  }
  return pts.join(" ");
}
