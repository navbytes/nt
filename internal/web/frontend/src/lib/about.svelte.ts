// Reactive flag for the About panel (opened from the command palette). Kept in
// its own module so the palette can trigger it without a handle on the overlay,
// mirroring the shortcuts/cheat-sheet pattern.
export const about = $state({ open: false });

export function openAbout(): void {
  about.open = true;
}
