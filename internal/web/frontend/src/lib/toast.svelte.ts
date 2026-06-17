// A small QUEUE of calm toasts for transient confirmations — chiefly the
// "Completed / Deleted — Undo" affordance after a destructive task write.
// Reversibility beats confirmation dialogs: act immediately, offer the way back.
//
// Informational toasts STACK (rapid actions no longer lose earlier messages),
// but the single-level Undo affordance stays single-flight: a NEW store-write
// clears any pending Undo toast (clearUndoToast / W10) so undo can never revert
// the wrong op. The visible stack is capped — older toasts beyond the cap drop.

export interface Toast {
  id: number;
  message: string;
  /** When set, the toast shows an Undo button that runs this. */
  undo?: () => void | Promise<void>;
}

/** How many toasts can be visible at once; older ones are evicted. */
export const MAX_VISIBLE = 3;

// The live stack, newest LAST. Toaster renders newest at the visible edge and
// shows at most MAX_VISIBLE. `current` is kept as a back-compat accessor for the
// most recent toast (used by tests/older callers).
export const toast = $state<{ items: Toast[]; current: Toast | null }>({
  items: [],
  current: null,
});

let seq = 0;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

function clearTimer(id: number) {
  const t = timers.get(id);
  if (t !== undefined) {
    clearTimeout(t);
    timers.delete(id);
  }
}

function syncCurrent() {
  toast.current = toast.items.length ? toast.items[toast.items.length - 1]! : null;
}

/**
 * Push a toast onto the stack. Returns its id. Auto-dismisses after `ttlMs`.
 * The stack is capped at MAX_VISIBLE — the oldest toast is evicted (and its
 * timer cleared) when a new one overflows the cap.
 */
export function showToast(message: string, undo?: () => void | Promise<void>, ttlMs = 6000): number {
  const id = ++seq;
  toast.items = [...toast.items, { id, message, undo }];
  // Evict the oldest while over the visible cap.
  while (toast.items.length > MAX_VISIBLE) {
    const dropped = toast.items[0]!;
    clearTimer(dropped.id);
    toast.items = toast.items.slice(1);
  }
  timers.set(
    id,
    setTimeout(() => dismissToast(id), ttlMs),
  );
  syncCurrent();
  return id;
}

/** Dismiss one toast by id, or (no id) clear the entire stack. */
export function dismissToast(id?: number): void {
  if (id === undefined) {
    for (const t of timers.values()) clearTimeout(t);
    timers.clear();
    toast.items = [];
  } else {
    clearTimer(id);
    toast.items = toast.items.filter((t) => t.id !== id);
  }
  syncCurrent();
}

// Drop any pending Undo toast as soon as a NEW write lands (W10). The undo engine
// is single-level — it reverts the LATEST write — so a lingering "Undo" after a
// subsequent write would silently revert the wrong operation. Callers invoke this
// from their post-write sync so dangling Undos disappear; plain confirmation
// toasts (no undo handler) stack and are left alone.
export function clearUndoToast(): void {
  let changed = false;
  for (const t of toast.items) {
    if (t.undo) {
      clearTimer(t.id);
      changed = true;
    }
  }
  if (changed) {
    toast.items = toast.items.filter((t) => !t.undo);
    syncCurrent();
  }
}
