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

// ---- per-folder open/collapse state (W31) -------------------------------------
// The notes tree refetches on every store change (SSE); folder open/collapse used
// to live in each TreeItem's local state, so it reset on every refetch. Persist it
// here (keyed by folder path) so a user's expanded branches survive refetches and
// reloads. Folders default to OPEN unless explicitly collapsed.
const FKEY = "nt-tree-collapsed";

function loadCollapsedFolders(): Set<string> {
  try {
    const raw = localStorage.getItem(FKEY);
    if (raw) return new Set(JSON.parse(raw) as string[]);
  } catch {
    /* ignore */
  }
  return new Set();
}

// A reactive record so dependents re-render when a folder toggles. We mirror the
// Set into this $state object (path → collapsed?) keyed by folder path.
export const treeCollapsed = $state<{ paths: Record<string, true> }>({
  paths: Object.fromEntries([...loadCollapsedFolders()].map((p) => [p, true as const])),
});

export function isFolderOpen(path: string): boolean {
  return !treeCollapsed.paths[path];
}

export function toggleFolder(path: string): void {
  if (treeCollapsed.paths[path]) {
    const { [path]: _drop, ...rest } = treeCollapsed.paths;
    treeCollapsed.paths = rest;
  } else {
    treeCollapsed.paths = { ...treeCollapsed.paths, [path]: true };
  }
  try {
    localStorage.setItem(FKEY, JSON.stringify(Object.keys(treeCollapsed.paths)));
  } catch {
    /* ignore */
  }
}
