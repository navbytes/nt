// Sidebar width + collapsed state, persisted to localStorage (mirrors the
// graphView/theme prefs pattern). Shell reads `sidebar` to drive the layout's
// --sidebar-w variable and the collapsed class; the resizer and the
// collapse/command-palette controls mutate it through the helpers below.

const WKEY = "nt-sidebar-w";
const CKEY = "nt-sidebar-collapsed";

export const MIN_W = 200;
export const MAX_W = 480;
export const DEFAULT_W = 260; // matches the original --sidebar-w

function clamp(px: number): number {
  return Math.max(MIN_W, Math.min(MAX_W, Math.round(px)));
}

function initialWidth(): number {
  try {
    const raw = localStorage.getItem(WKEY);
    if (raw) {
      const n = parseInt(raw, 10);
      if (Number.isFinite(n)) return clamp(n);
    }
  } catch {
    /* localStorage unavailable (private mode / quota) */
  }
  return DEFAULT_W;
}

function initialCollapsed(): boolean {
  try {
    return localStorage.getItem(CKEY) === "1";
  } catch {
    return false;
  }
}

export const sidebar = $state({ width: initialWidth(), collapsed: initialCollapsed() });

export function setWidth(px: number): void {
  sidebar.width = clamp(px);
  try {
    localStorage.setItem(WKEY, String(sidebar.width));
  } catch {
    /* ignore */
  }
}

export function resetWidth(): void {
  setWidth(DEFAULT_W);
}

export function toggleCollapsed(): void {
  sidebar.collapsed = !sidebar.collapsed;
  try {
    localStorage.setItem(CKEY, sidebar.collapsed ? "1" : "0");
  } catch {
    /* ignore */
  }
}
