// Pure helpers for WAI-ARIA tree keyboard navigation (roving tabindex). The DOM
// rendering only includes VISIBLE treeitems (collapsed folders don't render their
// children), so a flat in-DOM-order list of treeitems already respects collapse.
// These helpers do the index math over that flat list so it's unit-testable
// without a DOM.

/** One visible node, as projected from the rendered treeitems. */
export interface TreeNavItem {
  /** aria-level (1-based); a child sits at parent.level + 1. */
  level: number;
}

/** Index of the next/previous visible node, clamped at the ends (no wrap). */
export function stepIndex(count: number, current: number, dir: 1 | -1): number {
  if (count === 0) return -1;
  if (current < 0) return dir === 1 ? 0 : count - 1;
  return Math.min(Math.max(current + dir, 0), count - 1);
}

/**
 * Index of the parent of item `current`: the nearest PRECEDING item whose level
 * is exactly one less. Returns -1 for a top-level item (no parent).
 */
export function parentIndex(items: TreeNavItem[], current: number): number {
  if (current < 0 || current >= items.length) return -1;
  const lvl = items[current]!.level;
  for (let i = current - 1; i >= 0; i--) {
    if (items[i]!.level < lvl) return i;
  }
  return -1;
}

/**
 * Index of the first child of item `current`: the immediately following item iff
 * its level is exactly one deeper (only true when the folder is expanded and has
 * children, since collapsed children aren't in the visible list). Else -1.
 */
export function firstChildIndex(items: TreeNavItem[], current: number): number {
  if (current < 0 || current + 1 >= items.length) return -1;
  return items[current + 1]!.level === items[current]!.level + 1 ? current + 1 : -1;
}
