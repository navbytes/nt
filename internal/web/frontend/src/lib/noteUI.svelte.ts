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
