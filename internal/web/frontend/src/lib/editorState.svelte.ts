// Tracks the open-and-dirty editor so other parts of the app can avoid clobbering
// in-flight edits. SSE (lib/sse.ts) reads this to skip refetching the open note's
// ["raw"] cache on a `reload` event (which would stale the captured etag and
// guarantee a later 409); the editor instead surfaces a non-destructive
// "changed on disk — reload to merge" banner (W1). The router leave guard and the
// beforeunload handler read `dirty` to confirm before discarding edits (W2).

export const editorState = $state<{
  /** The handle currently open in the editor, or null when no editor is open. */
  handle: string | null;
  /** True when the open editor has unsaved buffer edits. */
  dirty: boolean;
  /** Set by SSE when the open note changed on disk (drives the merge banner). */
  changedOnDisk: boolean;
}>({ handle: null, dirty: false, changedOnDisk: false });

export function openEditor(handle: string): void {
  editorState.handle = handle;
  editorState.dirty = false;
  editorState.changedOnDisk = false;
}

export function setEditorDirty(dirty: boolean): void {
  editorState.dirty = dirty;
}

export function markChangedOnDisk(): void {
  if (editorState.handle) editorState.changedOnDisk = true;
}

export function clearChangedOnDisk(): void {
  editorState.changedOnDisk = false;
}

export function closeEditor(): void {
  editorState.handle = null;
  editorState.dirty = false;
  editorState.changedOnDisk = false;
}
