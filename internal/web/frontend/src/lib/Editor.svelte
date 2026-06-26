<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api, SaveConflict } from "./api";
  import { registerLeaveGuard } from "./router.svelte";
  import CodeMirror from "./CodeMirror.svelte";
  import { renderMermaidIn, observeTheme } from "./mermaid";
  import {
    openEditor,
    closeEditor,
    setEditorDirty,
    editorState,
    clearChangedOnDisk,
  } from "./editorState.svelte";

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
  // Conflict (409) state: when set, the bar offers Reload (discard buffer) vs
  // Overwrite (re-save over the on-disk version) instead of silently losing edits.
  let conflict = $state(false);
  // Transient "Saved" confirmation after an in-place save (W8).
  let savedFlash = $state(false);
  let savedTimer: ReturnType<typeof setTimeout> | undefined;

  // The last text we seeded the buffer with (on load or after a tag edit), so we
  // can tell whether the body has unsaved edits.
  let seededText = $state("");
  const dirty = $derived(loaded && buffer !== seededText);

  // Mobile pane toggle (finding 4): below 760px the global stylesheet collapses
  // the split to a single pane. Rather than silently dropping the preview, we
  // expose a source/preview switch; scoped styles below honour `mobilePane` to
  // decide which pane shows on a narrow viewport (desktop split is untouched).
  let mobilePane = $state<"src" | "preview">("src");

  // Publish open/dirty state so SSE skips clobbering our ["raw"] cache, and the
  // router leave guard + beforeunload can confirm before discarding edits (W1/W2).
  $effect(() => {
    openEditor(handle);
    const unguard = registerLeaveGuard(() => !dirty || confirm("Discard unsaved changes?"));
    return () => {
      unguard();
      closeEditor();
    };
  });
  $effect(() => {
    setEditorDirty(dirty);
  });
  // The SSE bridge flags this when the note changed on disk while we're editing.
  const changedOnDisk = $derived(editorState.changedOnDisk);

  // Pull the latest on-disk version into the buffer (reload-to-merge). Discards
  // any unsaved buffer edits, so it's only offered as an explicit action.
  async function reloadFromDisk() {
    try {
      const r = await api.raw(handle);
      buffer = r.text;
      etag = r.etag;
      seededText = r.text;
      conflict = false;
      error = "";
      clearChangedOnDisk();
    } catch (e) {
      error = `Couldn't reload: ${String(e)}`;
    }
  }

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
  // Each run aborts the previous in-flight request so a slow earlier response can
  // never overwrite a newer one (W23).
  $effect(() => {
    const text = buffer;
    if (!loaded) return;
    const ctrl = new AbortController();
    const id = setTimeout(async () => {
      try {
        previewHTML = await api.preview(text, ctrl.signal);
      } catch {
        /* aborted or transient; keep last good preview */
      }
    }, 250);
    return () => {
      clearTimeout(id);
      ctrl.abort();
    };
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
  async function editTags(add: string, remove = ""): Promise<boolean> {
    if (tagBusy) return false;
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
      return true;
    } catch (e) {
      error = String(e);
      return false;
    } finally {
      tagBusy = false;
    }
  }
  async function addTag(e: SubmitEvent) {
    e.preventDefault();
    const t = newTag.trim().replace(/^@/, "");
    if (!t) return;
    // Clear the field only once the add succeeds, so a failed request doesn't
    // lose what the user typed (W21).
    if (await editTags(t)) newTag = "";
  }

  // Persist the buffer WITHOUT closing (Cmd/Ctrl+S or the Save button). On success
  // the etag is refreshed so subsequent saves don't 409, and a brief "Saved" flag
  // confirms it; the editor stays open (W8). Returns whether the save succeeded so
  // done() can decide whether to close.
  async function saveInPlace(): Promise<boolean> {
    if (saving) return false;
    saving = true;
    error = "";
    try {
      await api.save(handle, buffer, etag);
      // Re-fetch the fresh etag so an in-place save can be repeated, and reseed
      // the dirty baseline. Await the note invalidation before any close so the
      // note view doesn't flash stale content (W22).
      const r = await api.raw(handle);
      etag = r.etag;
      seededText = buffer;
      conflict = false;
      clearChangedOnDisk();
      await qc.invalidateQueries({ queryKey: ["note", handle] });
      qc.invalidateQueries({ queryKey: ["raw", handle] });
      qc.invalidateQueries({ queryKey: ["notes"] });
      savedFlash = true;
      clearTimeout(savedTimer);
      savedTimer = setTimeout(() => (savedFlash = false), 1800);
      return true;
    } catch (e) {
      if (e instanceof SaveConflict) {
        // Don't lose the buffer: offer Reload (take disk) vs Overwrite (W19).
        conflict = true;
        error = "This note changed on disk since you opened it.";
      } else {
        error = String(e);
      }
      return false;
    } finally {
      saving = false;
    }
  }

  // Overwrite the on-disk version with the current buffer after a conflict:
  // re-fetch the latest etag, then save against it (the user chose to keep their
  // edits over what landed on disk). The buffer is never discarded (W19).
  async function overwrite() {
    if (saving) return;
    saving = true;
    error = "";
    try {
      const r = await api.raw(handle);
      etag = r.etag;
      await api.save(handle, buffer, etag);
      const fresh = await api.raw(handle);
      etag = fresh.etag;
      seededText = buffer;
      conflict = false;
      clearChangedOnDisk();
      await qc.invalidateQueries({ queryKey: ["note", handle] });
      qc.invalidateQueries({ queryKey: ["raw", handle] });
      qc.invalidateQueries({ queryKey: ["notes"] });
      savedFlash = true;
      clearTimeout(savedTimer);
      savedTimer = setTimeout(() => (savedFlash = false), 1800);
    } catch (e) {
      error = `Couldn't overwrite: ${String(e)}`;
    } finally {
      saving = false;
    }
  }

  // Done = save then close (only closes if the save succeeded, so a conflict
  // keeps you in the editor with your buffer intact).
  async function done() {
    if (await saveInPlace()) onClose();
  }

  // Close, confirming first if there are unsaved edits (Cancel / Escape) (W2).
  function requestClose() {
    if (dirty && !confirm("Discard unsaved changes?")) return;
    onClose();
  }

  // Guard a hard reload / tab close while dirty (W2).
  $effect(() => {
    function beforeUnload(e: BeforeUnloadEvent) {
      if (dirty) {
        e.preventDefault();
        e.returnValue = ""; // required by some browsers to trigger the prompt
      }
    }
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  });

</script>

<div class="editor">
  <div class="editor__bar">
    <div class="pillbar">
      <button
        class="pillbar__btn pillbar__btn--accent"
        class:pillbar__btn--ok={savedFlash}
        onclick={saveInPlace}
        disabled={saving || !loaded}
      >
        {saving ? "Saving…" : savedFlash ? "Saved ✓" : "Save"}
      </button>
      <button class="pillbar__btn" onclick={done} disabled={saving || !loaded} title="Save and close">Done</button>
      <span class="pillbar__sep"></span>
      <button class="pillbar__btn" onclick={requestClose}>Cancel</button>
    </div>
    <span class="kbd">⌘/Ctrl+S</span>
    <!-- Narrow-viewport source/preview switch (finding 4): only shown when the
         split collapses to one pane, so the preview is never silently dropped. -->
    <div class="paneswitch" role="group" aria-label="Editor pane">
      <button
        type="button"
        class="paneswitch__btn"
        class:paneswitch__btn--on={mobilePane === "src"}
        aria-pressed={mobilePane === "src"}
        onclick={() => (mobilePane = "src")}>Source</button
      >
      <button
        type="button"
        class="paneswitch__btn"
        class:paneswitch__btn--on={mobilePane === "preview"}
        aria-pressed={mobilePane === "preview"}
        onclick={() => (mobilePane = "preview")}>Preview</button
      >
    </div>
    <!-- Status + errors announced to assistive tech (W16). -->
    <span class="editor__status" aria-live="polite">
      {#if savedFlash}Saved{:else if dirty}Unsaved changes{/if}
    </span>
    {#if error}<span class="error" role="alert">{error}</span>{/if}
  </div>

  {#if conflict}
    <div class="editor__banner editor__banner--warn" role="alert">
      <span>This note changed on disk since you opened it.</span>
      <button class="pillbar__btn" onclick={reloadFromDisk} disabled={saving}>Reload (discard my edits)</button>
      <button class="pillbar__btn pillbar__btn--accent" onclick={overwrite} disabled={saving}>Overwrite with my edits</button>
    </div>
  {:else if changedOnDisk}
    <div class="editor__banner" role="status">
      <span>Changed on disk — reload to merge.</span>
      <button class="pillbar__btn" onclick={reloadFromDisk} disabled={saving || dirty} title={dirty ? "Save or discard your edits first" : "Reload from disk"}>Reload</button>
    </div>
  {/if}

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
    <div class="editor__panes" data-pane={mobilePane}>
      {#if loaded}
        <div class="editor__src">
          <CodeMirror
            value={buffer}
            onChange={(v) => (buffer = v)}
            onSave={saveInPlace}
            onEscape={requestClose}
            getNotes={() => $notesQ.data?.index ?? []}
          />
        </div>
      {/if}
      <div class="editor__preview prose" bind:this={previewEl}>{@html previewHTML}</div>
    </div>
  {/if}
</div>

<style>
  /* Success pulse on the Save button after an in-place save, so it visibly
     confirms ("Saved ✓") instead of looking like it did nothing — Save keeps
     you in the editor, Done saves and closes. */
  .pillbar__btn--ok {
    background: var(--green-color);
    border-color: var(--green-color);
    color: #fff;
  }

  /* Finding 5: let the editor flex within its column instead of relying on the
     magic calc(100vh - 140px) as a *fixed* height. With conflict/on-disk banners
     + the tag bar + backlinks on a short viewport the fixed height (plus the
     min-height floor) squeezed/overflowed the CodeMirror pane. We keep the
     viewport sizing as the *preferred* basis (desktop feel unchanged) but let it
     shrink (min-height:0) and grow to fill a flex parent — and the panes already
     flex:1, so the chrome above them eats from the panes, never overflows. */
  .editor {
    /* Flex into a flex-column parent when there is one; otherwise fall back to a
       viewport-relative *definite* height (dvh tracks mobile browser chrome) so
       the panes below still have something to flex against. Crucially we drop the
       global min-height:420px floor — that floor is what overflowed short
       viewports — and allow shrink via min-height:0. */
    flex: 1 1 auto;
    min-height: 0;
    height: calc(100dvh - 140px);
  }
  .editor__panes {
    /* Claim the leftover space below the (variable-height) banners/tag bar so
       reducing the available height squeezes the panes rather than overflowing. */
    flex: 1 1 auto;
    min-height: 0;
  }

  /* Finding 3: the save-status microlabel sits on the editor fill where --muted
     is sub-AA; lift it to --fg-soft (the placeholder uses the same surface). */
  .editor :global(.editor__status) {
    color: var(--fg-soft);
  }

  /* Finding 15: the tag-remove × is a ~14px target — too small for touch. Give it
     a ≥24px hit area without changing its visual weight. */
  .editor :global(.tagchip__x) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 24px;
    min-height: 24px;
    margin: -4px -6px -4px 0; /* absorb the padding so the chip doesn't grow */
  }

  /* Finding 4: source/preview switch. Hidden on the desktop split (both panes
     visible); revealed only where the global stylesheet collapses to one pane. */
  .paneswitch {
    display: none;
    gap: 2px;
    padding: 2px;
    margin-left: auto;
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    border-radius: var(--radius-sm);
    box-shadow: 0 0 0 0.5px var(--separator);
  }
  .paneswitch__btn {
    padding: 3px 10px;
    background: transparent;
    border: none;
    border-radius: calc(var(--radius-sm) - 1px);
    color: var(--label-secondary);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
  }
  .paneswitch__btn--on {
    background: var(--bg-elevated);
    color: var(--fg);
    box-shadow: var(--shadow-control);
  }

  @media (max-width: 760px) {
    .paneswitch {
      display: inline-flex;
    }
    /* The aria-live status would push the switch off the row; let it wrap. */
    .editor :global(.editor__status) {
      flex-basis: 100%;
      order: 5;
    }
    /* Show exactly the chosen pane (overriding app.css's blanket preview hide),
       and let it fill the collapsed single column. */
    .editor__panes[data-pane="src"] :global(.editor__preview) {
      display: none;
    }
    .editor__panes[data-pane="preview"] :global(.editor__preview) {
      display: block;
    }
    .editor__panes[data-pane="preview"] .editor__src {
      display: none;
    }
  }
</style>
