<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api, SaveConflict } from "./api";
  import CodeMirror from "./CodeMirror.svelte";
  import { renderMermaidIn, observeTheme } from "./mermaid";

  let { handle, onClose }: { handle: string; onClose: () => void } = $props();

  const qc = useQueryClient();
  const rawQ = createQuery({ queryKey: ["raw", handle], queryFn: () => api.raw(handle) });
  // The note view (cached from the page) carries backlinks + task refs — keep
  // them visible while editing so you have context for what links here (W8).
  const noteQ = createQuery({ queryKey: ["note", handle], queryFn: () => api.note(handle) });
  // Cached note index powers [[ wikilink autocomplete in the editor.
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  let buffer = $state("");
  let etag = $state("");
  let loaded = $state(false);
  let previewHTML = $state("");
  let saving = $state(false);
  let error = $state("");

  // The last text we seeded the buffer with (on load or after a tag edit), so we
  // can tell whether the body has unsaved edits.
  let seededText = $state("");
  const dirty = $derived(loaded && buffer !== seededText);

  // Seed the buffer once the raw note loads (don't clobber later edits).
  $effect(() => {
    if ($rawQ.data && !loaded) {
      buffer = $rawQ.data.text;
      etag = $rawQ.data.etag;
      seededText = $rawQ.data.text;
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

  // The preview is server-rendered HTML, so ```mermaid``` fences arrive as
  // unrendered <div class="mermaid"> blocks — run Mermaid over the pane after
  // each preview update (and on theme toggle), exactly like the note page.
  let previewEl: HTMLElement | undefined = $state();
  $effect(() => {
    void previewHTML; // re-run after {@html} patches the pane
    const el = previewEl;
    if (!el) return;
    const id = setTimeout(() => void renderMermaidIn(el), 0);
    return () => clearTimeout(id);
  });
  $effect(() => {
    const el = previewEl;
    if (!el) return;
    return observeTheme(() => void renderMermaidIn(el));
  });

  // ---- frontmatter tags (W9) ----------------------------------------------
  // Structured tag editing in the context bar so you don't hand-edit YAML. Edits
  // the frontmatter only (body untouched) via its own endpoint; on success the
  // ["note"]/["raw"] caches refresh so the buffer's YAML reflects the change.
  let newTag = $state("");
  let tagBusy = $state(false);
  const tags = $derived($noteQ.data?.tags ?? []);
  async function editTags(add: string, remove = "") {
    if (tagBusy) return;
    tagBusy = true;
    try {
      await api.noteTags(handle, add, remove);
      await qc.invalidateQueries({ queryKey: ["note", handle] });
      await qc.invalidateQueries({ queryKey: ["raw", handle] });
      await qc.invalidateQueries({ queryKey: ["notes"] });
      // The on-disk frontmatter changed; reseed the buffer so a later save
      // doesn't clobber the new tags with the stale YAML we opened with.
      const r = await api.raw(handle);
      buffer = r.text;
      etag = r.etag;
      seededText = r.text;
    } catch (e) {
      error = String(e);
    } finally {
      tagBusy = false;
    }
  }
  function addTag(e: SubmitEvent) {
    e.preventDefault();
    const t = newTag.trim().replace(/^@/, "");
    if (t) {
      newTag = "";
      void editTags(t);
    }
  }

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

  {#if $noteQ.data}
    <div class="editor__context editor__tags">
      <span class="editor__context-label">Tags</span>
      {#each tags as tag (tag)}
        <span class="tagchip">
          #{tag}
          <button
            class="tagchip__x"
            disabled={tagBusy || dirty}
            title={dirty ? "Save your edits first" : `Remove @${tag}`}
            aria-label={`Remove tag ${tag}`}
            onclick={() => editTags("", tag)}>×</button
          >
        </span>
      {/each}
      <form class="tagadd" onsubmit={addTag}>
        <input
          bind:value={newTag}
          placeholder={dirty ? "save first…" : "add tag…"}
          aria-label="Add a tag"
          title={dirty ? "Save your buffer edits before editing tags" : "Add a tag"}
          disabled={tagBusy || dirty}
          autocomplete="off"
        />
      </form>
    </div>
  {/if}

  {#if $noteQ.data && ($noteQ.data.backlinks.length > 0 || $noteQ.data.taskRefs.length > 0)}
    <div class="editor__context">
      {#if $noteQ.data.backlinks.length > 0}
        <span class="editor__context-label">Linked from</span>
        {#each $noteQ.data.backlinks as b (b.url)}
          <a class="editor__chip" href={b.url} target="_blank" rel="external" title={b.text}>{b.title}</a>
        {/each}
      {/if}
      {#if $noteQ.data.taskRefs.length > 0}
        <span class="editor__context-label">{$noteQ.data.taskRefs.length} task{$noteQ.data.taskRefs.length > 1 ? "s" : ""}</span>
      {/if}
    </div>
  {/if}

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
      <div class="editor__preview prose" bind:this={previewEl}>{@html previewHTML}</div>
    </div>
  {/if}
</div>
