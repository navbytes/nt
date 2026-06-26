// Cross-component signals for actions triggered from the command palette (which
// has no direct handle on the open NoteView or the Sidebar). Monotonic counters
// the owning component watches: NoteView opens its "move" picker, the Sidebar
// opens its inline new-note input.
export const noteUI = $state({ moveRequest: 0, newNoteRequest: 0 });

export function requestMoveNote(): void {
  noteUI.moveRequest++;
}

export function requestNewNote(): void {
  noteUI.newNoteRequest++;
}

// Note reading width, persisted (mirrors the sidebar/theme prefs pattern). The
// note body defaults to a comfortable ~72ch measure; "wide" lifts that cap so a
// note fills the available column on a large window. NoteView reads `readWidth`
// to toggle a class on the article; the toolbar button flips it.
const WIDE_KEY = "nt-note-wide";

function initialWide(): boolean {
  try {
    return localStorage.getItem(WIDE_KEY) === "1";
  } catch {
    return false;
  }
}

export const readWidth = $state({ wide: initialWide() });

export function toggleReadWidth(): void {
  readWidth.wide = !readWidth.wide;
  try {
    localStorage.setItem(WIDE_KEY, readWidth.wide ? "1" : "0");
  } catch {
    /* localStorage unavailable (private mode / quota) — non-fatal */
  }
}
