// Global keyboard layer — the "feels like a real tool" keys that don't belong to
// any one view: a `g`-prefixed go-to chord (g t = Today, g a = Tasks…), `/` to
// search, `c` to capture, and `?` for the shortcut cheat-sheet. The command
// palette (⌘K) owns its own keys; this module is everything else.
//
// The pure pieces (goTarget, isTextEntry) are exported so they can be unit-tested
// without a DOM event loop; the wiring lives in Shortcuts.svelte.

// Reactive flag for the `?` cheat-sheet overlay.
export const shortcuts = $state({ open: false });

// Where each `g`-prefixed go-to chord lands. Mirrors the sidebar + palette nav so
// muscle memory transfers between mouse and keyboard. Second keys are mnemonic
// (t·a·r·n·d·g·v) and surfaced in the cheat-sheet, so they don't have to be guessed.
export const GO_CHORDS: { key: string; path: string; label: string }[] = [
  { key: "t", path: "/", label: "Today" },
  { key: "a", path: "/tasks", label: "Tasks (agenda)" },
  { key: "r", path: "/review", label: "Review" },
  { key: "n", path: "/notes", label: "Notes" },
  { key: "d", path: "/journal", label: "Daily note" },
  { key: "g", path: "/graph", label: "Graph" },
  { key: "v", path: "/activity", label: "Activity" },
];

// goTarget resolves the second key of a `g` chord to a route, or null if the key
// isn't a go-to target (so the chord is abandoned rather than firing blindly).
export function goTarget(key: string): string | null {
  const hit = GO_CHORDS.find((g) => g.key === key.toLowerCase());
  return hit ? hit.path : null;
}

// isTextEntry is true when the user is typing somewhere a bare letter should reach
// the field, not trigger a shortcut (inputs, textareas, selects, contenteditable).
export function isTextEntry(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  if (el.isContentEditable) return true;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}
