// A single calm toast (latest wins) for transient confirmations — chiefly the
// "Completed / Deleted — Undo" affordance after a destructive task write.
// Reversibility beats confirmation dialogs: act immediately, offer the way back.

export interface Toast {
  id: number;
  message: string;
  /** When set, the toast shows an Undo button that runs this. */
  undo?: () => void | Promise<void>;
}

export const toast = $state<{ current: Toast | null }>({ current: null });

let seq = 0;
let timer: ReturnType<typeof setTimeout> | undefined;

export function showToast(message: string, undo?: () => void | Promise<void>, ttlMs = 6000) {
  toast.current = { id: ++seq, message, undo };
  clearTimeout(timer);
  timer = setTimeout(() => (toast.current = null), ttlMs);
}

export function dismissToast() {
  clearTimeout(timer);
  toast.current = null;
}

// Drop a stale Undo toast as soon as a NEW write lands (W10). The undo engine is
// single-level — it reverts the LATEST write — so a lingering "Undo" after a
// subsequent write would silently revert the wrong operation. Callers invoke
// this from their post-write sync so the dangling Undo disappears; a plain
// confirmation toast (no undo handler) is left alone.
export function clearUndoToast() {
  if (toast.current?.undo) {
    clearTimeout(timer);
    toast.current = null;
  }
}
