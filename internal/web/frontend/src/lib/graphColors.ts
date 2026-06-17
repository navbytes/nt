import type { FGNode } from "./graph";
import type { ColorBy } from "./graphView.svelte";

// Two parallel categorical palettes, indexed identically so a key keeps the same
// SLOT across themes (only the hue's lightness flips). The slots are ordered so
// the first ~6 — the common case — are maximally separated in hue + lightness and
// survive colour-blindness; near-duplicate hues are pushed to the tail, where the
// parallel shape channel does the disambiguating. (audit #1, #2)
//
// DARK palette (Tokyo Night) is tuned for the dark canvas (#202024): every entry
// clears 3:1 there (measured 6.1–13.8:1). On the LIGHT canvas (#ffffff) these
// pale to 1.2–2.7:1, so LIGHT is a set of darker/saturated variants (L* ≤ 58)
// that each clear 3:1 on white (measured 3.49–5.67:1).
//
//                 blue       green      orange     purple     cyan       pink
const PALETTE_DARK = [
  "#7aa2f7", "#9ece6a", "#ff9e64", "#bb9af7", "#2ac3de", "#f7768e",
  // tail (lean on shape past ~6): amber, teal, lavender, sky, ice, magenta
  "#e0af68", "#73daca", "#c0caf5", "#7dcfff", "#b4f9f8", "#ff75a0",
];
const PALETTE_LIGHT = [
  "#2f6fde", "#3d8b1f", "#c2620a", "#8a4fd8", "#0e8aa6", "#d11d4e",
  "#9a6a00", "#1597a8", "#5a6acf", "#c0392b", "#b03a8a", "#557000",
];

function palette(dark: boolean): string[] {
  return dark ? PALETTE_DARK : PALETTE_LIGHT;
}

const UNCATEGORIZED_DARK = "#8b8b8b";
const UNCATEGORIZED_LIGHT = "#6b6b6b"; // darker grey clears 3:1 on white
export function uncategorizedColor(dark = true): string {
  return dark ? UNCATEGORIZED_DARK : UNCATEGORIZED_LIGHT;
}

// Slot assignment is theme-independent: a key is assigned the next free SLOT
// once, then resolved against whichever palette the active theme asks for. This
// keeps a folder/tag the "same colour" across a theme switch (just light/dark).
const assignedSlot = new Map<string, number>();

function slotForKey(key: string): number {
  let s = assignedSlot.get(key);
  if (s === undefined) {
    s = assignedSlot.size % PALETTE_DARK.length;
    assignedSlot.set(key, s);
  }
  return s;
}

function colorForKey(key: string, dark: boolean): string {
  if (!key) return uncategorizedColor(dark);
  return palette(dark)[slotForKey(key) % PALETTE_DARK.length] as string;
}

// dimensionValue is the single value used to colour a node under a given
// dimension (first tag when colouring by tag; a note can carry many).
export function dimensionValue(n: FGNode, colorBy: ColorBy): string {
  switch (colorBy) {
    case "tag":
      return n.tags[0] ?? "";
    case "source":
      return n.source ?? "";
    case "none":
      return "";
    default:
      return n.folder;
  }
}

export function nodeColor(n: FGNode, colorBy: ColorBy, dark = true): string {
  if (colorBy === "none") return palette(dark)[0] as string;
  const v = dimensionValue(n, colorBy);
  if (!v) return uncategorizedColor(dark); // empty folder/tag/source → same grey as legend
  return colorForKey(colorBy + ":" + v, dark);
}

// legendEntries lists the distinct {value,color} pairs present in the data for
// the active dimension, sorted, for the interactive legend.
// withAlpha returns an rgba() string for a #rgb / #rrggbb color at alpha a. The
// graph palette + accent tokens are all hex; this lets the canvas tint nodes and
// links at runtime without re-deriving colors. Non-hex input is returned as-is.
export function withAlpha(hex: string, a: number): string {
  let h = hex.trim();
  if (h[0] === "#") h = h.slice(1);
  if (h.length === 3) h = h[0]! + h[0]! + h[1]! + h[1]! + h[2]! + h[2]!;
  if (h.length !== 6) return hex;
  const n = parseInt(h, 16);
  if (Number.isNaN(n)) return hex;
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}

// ---- edge (relationship) colors -------------------------------------------
// The graph's edges carry a `kind` (server: GraphLink.Kind) naming the predicate
// family. Coloring by it lets you trace how memory connects: knowledge↔knowledge
// vs. work→knowledge vs. task dependencies. Fixed (not palette-cycled) so the
// meaning is stable across stores.
// Dark-canvas tuned; light variants are darker/saturated so a typed edge clears
// ~3:1 on white too (audit #3).
const LINK_KIND_COLOR_DARK: Record<string, string> = {
  wikilink: "#7aa2f7", // note ↔ note (blue)
  task: "#9ece6a", // task → note reference (green)
  parent: "#bb9af7", // sub-task → parent (purple)
  blocks: "#f7768e", // blocker → blocked (red)
  discovered: "#e0af68", // discovered-from (amber)
};
const LINK_KIND_COLOR_LIGHT: Record<string, string> = {
  wikilink: "#2f6fde",
  task: "#3d8b1f",
  parent: "#8a4fd8",
  blocks: "#c0392b",
  discovered: "#9a6a00",
};
function linkKindColors(dark: boolean): Record<string, string> {
  return dark ? LINK_KIND_COLOR_DARK : LINK_KIND_COLOR_LIGHT;
}
const LINK_KIND_LABEL: Record<string, string> = {
  wikilink: "Wikilink",
  task: "Task ref",
  parent: "Parent",
  blocks: "Blocks",
  discovered: "Discovered",
};

export function linkKindColor(kind?: string, dark = true): string {
  return linkKindColors(dark)[kind || "wikilink"] ?? uncategorizedColor(dark);
}

// linkKindLegend lists the distinct edge kinds present, in a stable order, for
// the edge legend (only shown when "color edges by type" is on).
export function linkKindLegend(
  kinds: (string | undefined)[],
  dark = true,
): { value: string; label: string; color: string }[] {
  const order = ["wikilink", "task", "parent", "blocks", "discovered"];
  const present = new Set(kinds.map((k) => k || "wikilink"));
  const colors = linkKindColors(dark);
  return order
    .filter((k) => present.has(k))
    .map((k) => ({ value: k, label: LINK_KIND_LABEL[k] ?? k, color: colors[k]! }));
}

export function legendEntries(
  nodes: FGNode[],
  colorBy: ColorBy,
  dark = true,
): { value: string; label: string; color: string }[] {
  if (colorBy === "none") return [];
  const seen = new Set<string>();
  for (const n of nodes) seen.add(dimensionValue(n, colorBy));
  return [...seen]
    .sort((a, b) => a.localeCompare(b))
    .map((value) => ({
      value,
      label: value || "(none)",
      color: value ? colorForKey(colorBy + ":" + value, dark) : uncategorizedColor(dark),
    }));
}
