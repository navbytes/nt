// Shared open-state for the command palette so both the global ⌘K/Ctrl+K
// shortcut and the topbar button drive the same instance.
export const palette = $state({ open: false });

export function openPalette(): void {
  palette.open = true;
}

export function closePalette(): void {
  palette.open = false;
}
