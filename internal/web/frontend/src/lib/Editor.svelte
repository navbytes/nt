<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api, SaveConflict } from "./api";
  import CodeMirror from "./CodeMirror.svelte";

  let { handle, onClose }: { handle: string; onClose: () => void } = $props();

  const qc = useQueryClient();
  const rawQ = createQuery({ queryKey: ["raw", handle], queryFn: () => api.raw(handle) });
  // Cached note index powers [[ wikilink autocomplete in the editor.
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  let buffer = $state("");
  let etag = $state("");
  let loaded = $state(false);
  let previewHTML = $state("");
  let saving = $state(false);
  let error = $state("");

  // Seed the buffer once the raw note loads (don't clobber later edits).
  $effect(() => {
    if ($rawQ.data && !loaded) {
      buffer = $rawQ.data.text;
      etag = $rawQ.data.etag;
      loaded = true;
    }
  });

  // Debounced live preview through the same goldmark path the note page uses.
  $effect(() => {
    const text = buffer;
    if (!loaded) return;
    const id = setTimeout(async () => {
      try {
        previewHTML = await api.preview(text);
      } catch {
        /* transient; keep last good preview */
      }
    }, 250);
    return () => clearTimeout(id);
  });

  async function save() {
    saving = true;
    error = "";
    try {
      await api.save(handle, buffer, etag);
      qc.invalidateQueries({ queryKey: ["note", handle] });
      qc.invalidateQueries({ queryKey: ["raw", handle] });
      qc.invalidateQueries({ queryKey: ["notes"] });
      onClose();
    } catch (e) {
      error =
        e instanceof SaveConflict
          ? "This note changed on disk since you opened it — close and reopen to merge."
          : String(e);
    } finally {
      saving = false;
    }
  }

</script>

<div class="editor">
  <div class="editor__bar">
    <button class="btn" onclick={save} disabled={saving || !loaded}>
      {saving ? "Saving…" : "Save"}
    </button>
    <button class="btn btn--ghost" onclick={onClose}>Cancel</button>
    <span class="kbd">⌘/Ctrl+S</span>
    {#if error}<span class="error">{error}</span>{/if}
  </div>

  {#if $rawQ.isPending}
    <p class="muted">Loading…</p>
  {:else if $rawQ.error}
    <p class="error">Couldn't open this note for editing.</p>
  {:else}
    <div class="editor__panes">
      {#if loaded}
        <div class="editor__src">
          <CodeMirror
            value={buffer}
            onChange={(v) => (buffer = v)}
            onSave={save}
            onEscape={onClose}
            getNotes={() => $notesQ.data?.index ?? []}
          />
        </div>
      {/if}
      <div class="editor__preview prose">{@html previewHTML}</div>
    </div>
  {/if}
</div>
