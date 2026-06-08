import type { FGNode } from "./graph";
import type { ColorBy } from "./graphView.svelte";

// Tokyo Night accent palette. Folders/tags/sources map deterministically onto
// it (stable across renders), keyed per dimension so switching "color by" gives
// each dimension its own consistent assignment and a matching legend.
const PALETTE = [
  "#7aa2f7", "#9ece6a", "#ff9e64", "#f7768e",
  "#7dcfff", "#bb9af7", "#e0af68", "#2ac3de",
  "#73daca", "#b4f9f8", "#c0caf5", "#ff75a0",
];

const UNCATEGORIZED = "#8b8b8b";

const assigned = new Map<string, string>();

function colorForKey(key: string): string {
  if (!key) return UNCATEGORIZED;
  let c = assigned.get(key);
  if (!c) {
    c = PALETTE[assigned.size % PALETTE.length] as string;
    assigned.set(key, c);
  }
  return c;
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

export function nodeColor(n: FGNode, colorBy: ColorBy): string {
  if (colorBy === "none") return PALETTE[0] as string;
  const v = dimensionValue(n, colorBy);
  if (!v) return UNCATEGORIZED; // empty folder/tag/source → same grey as the legend
  return colorForKey(colorBy + ":" + v);
}

// legendEntries lists the distinct {value,color} pairs present in the data for
// the active dimension, sorted, for the interactive legend.
export function legendEntries(
  nodes: FGNode[],
  colorBy: ColorBy,
): { value: string; label: string; color: string }[] {
  if (colorBy === "none") return [];
  const seen = new Set<string>();
  for (const n of nodes) seen.add(dimensionValue(n, colorBy));
  return [...seen]
    .sort((a, b) => a.localeCompare(b))
    .map((value) => ({
      value,
      label: value || "(none)",
      color: value ? colorForKey(colorBy + ":" + value) : UNCATEGORIZED,
    }));
}
