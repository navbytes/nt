// Cross-component signal for note-page actions triggered from the command
// palette (which has no direct handle on the open NoteView). A monotonic counter
// the NoteView watches to open its "move" picker.
export const noteUI = $state({ moveRequest: 0 });

export function requestMoveNote(): void {
  noteUI.moveRequest++;
}
