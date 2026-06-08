// Shared reactive state for the graph view. Centralising it here (rather than
// threading dozens of props through GraphControls/Details/ContextMenu) keeps the
// orchestrator readable: every panel imports `view` and mutates it directly,
// and Graph.svelte reacts. The persistable subset (display + physics prefs) is
// mirrored to localStorage so a user's tuned graph survives reloads.

export type ColorBy = "folder" | "tag" | "source" | "none";
export type Mode = "global" | "local";

const KEY = "nt-graph-view";

// Fields that persist across reloads (look/physics prefs, not transient
// selection/filter state — those reset each visit so the graph opens clean).
const PERSIST_KEYS = [
  "colorBy",
  "showLabels",
  "showArrows",
  "particles",
  "repel",
  "linkDistance",
  "centerGravity",
  "labelThreshold",
  "depth",
] as const;

interface ViewState {
  mode: Mode;
  depth: number;
  rootId: string | null;
  selectedId: string | null;
  search: string;
  colorBy: ColorBy;
  filterFolders: string[];
  filterTags: string[];
  filterSources: string[];
  hideOrphans: boolean;
  showLabels: boolean;
  showArrows: boolean;
  particles: boolean;
  frozen: boolean;
  repel: number;
  linkDistance: number;
  centerGravity: number;
  labelThreshold: number;
}

const defaults: ViewState = {
  mode: "global",
  depth: 2,
  rootId: null,
  selectedId: null,
  search: "",
  colorBy: "folder",
  filterFolders: [],
  filterTags: [],
  filterSources: [],
  hideOrphans: false,
  showLabels: true,
  showArrows: false,
  particles: false,
  frozen: false,
  repel: -120,
  linkDistance: 30,
  centerGravity: 0.08,
  labelThreshold: 1.2,
};

function loadPersisted(): Partial<ViewState> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return {};
    const saved = JSON.parse(raw) as Record<string, unknown>;
    const out: Record<string, unknown> = {};
    for (const k of PERSIST_KEYS) if (k in saved) out[k] = saved[k];
    return out as Partial<ViewState>;
  } catch {
    return {};
  }
}

export const view = $state<ViewState>({ ...defaults, ...loadPersisted() });

// savePersisted serialises only the persistable subset. Call after any change to
// a persisted field (Graph.svelte runs it from an $effect that reads them all).
export function savePersisted(): void {
  try {
    const out: Record<string, unknown> = {};
    for (const k of PERSIST_KEYS) out[k] = view[k];
    localStorage.setItem(KEY, JSON.stringify(out));
  } catch {
    // localStorage unavailable (private mode / quota) — non-fatal.
  }
}

// toggleIn flips a value's membership in one of the filter arrays, returning a
// new array so the $state reference changes and dependents re-run.
export function toggleIn(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

// resetFilters clears transient filter/search state (not look/physics prefs).
export function resetFilters(): void {
  view.search = "";
  view.filterFolders = [];
  view.filterTags = [];
  view.filterSources = [];
  view.hideOrphans = false;
}

// enterLocal roots the local graph on a node; exitLocal returns to global.
export function enterLocal(id: string): void {
  view.mode = "local";
  view.rootId = id;
  view.selectedId = id;
}

export function exitLocal(): void {
  view.mode = "global";
  view.rootId = null;
}
