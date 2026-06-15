<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "./api";
  import Icon from "./Icon.svelte";
  import type { FGNode } from "./graph";

  let {
    node,
    pinned,
    expanded,
    onOpen,
    onFocusLocal,
    onTogglePin,
    onToggleExpand,
    onClose,
  }: {
    node: FGNode;
    pinned: boolean;
    expanded: boolean;
    onOpen: () => void;
    onFocusLocal: () => void;
    onTogglePin: () => void;
    onToggleExpand: () => void;
    onClose: () => void;
  } = $props();

  // Lazily fetch the note for the preview + backlink count. Keyed on the node id
  // so selecting a different node refetches; reuses the same cache as NoteView.
  const noteQ = $derived(
    createQuery({ queryKey: ["note", node.id], queryFn: () => api.note(node.id) }),
  );

  // Plain-text preview from the server-rendered HTML (strips tags, drops a
  // leading H1 that duplicates the title).
  function preview(html: string): string {
    if (!html || typeof DOMParser === "undefined") return "";
    const doc = new DOMParser().parseFromString(html, "text/html");
    doc.querySelector("h1")?.remove();
    const text = (doc.body.textContent ?? "").replace(/\s+/g, " ").trim();
    return text.length > 240 ? text.slice(0, 240) + "…" : text;
  }
</script>

<aside class="gdetails" aria-label="Selected note">
  <div class="gdetails__head">
    <div class="gdetails__crumb">{node.folder || "root"}</div>
    <button class="icon-btn" title="Close (Esc)" aria-label="Close details" onclick={onClose}><Icon name="close" /></button>
  </div>
  <h2 class="gdetails__title">{node.title}</h2>

  <div class="gdetails__meta">
    {#if node.source}<span class="src">{node.source}</span>{/if}
    <span class="muted small">{node.deg} connection{node.deg === 1 ? "" : "s"}</span>
    {#if $noteQ.data}<span class="muted small">· {$noteQ.data.backlinks.length} backlink{$noteQ.data.backlinks.length === 1 ? "" : "s"}</span>{/if}
    {#if pinned}<span class="pin-badge" title="Pinned">📌 pinned</span>{/if}
  </div>

  {#if node.tags.length}
    <div class="gdetails__tags">
      {#each node.tags as t (t)}<span class="chip">#{t}</span>{/each}
    </div>
  {/if}

  {#if $noteQ.isPending}
    <p class="muted small">Loading preview…</p>
  {:else if $noteQ.data}
    <p class="gdetails__preview">{preview($noteQ.data.bodyHTML)}</p>
  {/if}

  <div class="gdetails__actions">
    <button class="btn btn--sm" onclick={onOpen}>Open note</button>
    <button class="btn btn--ghost btn--sm gdetails__act" onclick={onFocusLocal}>
      <Icon name="focus" size={14} /> Focus local
    </button>
    <button
      class="btn btn--ghost btn--sm gdetails__act"
      title="Reveal this node's neighbors (shift-click a node does this too)"
      onclick={onToggleExpand}>
      <span class="gdetails__chev" class:gdetails__chev--open={expanded}><Icon name="chevron-right" size={13} /></span>
      {expanded ? "Collapse" : "Expand"} neighbors</button
    >
    <button class="btn btn--ghost btn--sm" onclick={onTogglePin}>{pinned ? "Unpin" : "Pin"}</button>
  </div>
</aside>

<style>
  .gdetails {
    position: absolute;
    top: 12px;
    right: 12px;
    z-index: 20;
    width: 300px;
    max-width: 40vw;
    background: var(--bg-elevated);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-popover);
    box-shadow: var(--shadow-popover);
    padding: 12px 14px;
  }
  .gdetails__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .gdetails__crumb {
    font-size: 0.72rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
  }
  .gdetails__title {
    margin: 2px 0 6px;
    font-size: 1.05rem;
    line-height: 1.25;
    color: var(--fg);
  }
  .gdetails__meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
  }
  .gdetails__tags {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    margin-bottom: 8px;
  }
  .gdetails__preview {
    font-size: 0.83rem;
    color: var(--fg-soft);
    line-height: 1.5;
    margin: 0 0 12px;
  }
  .gdetails__actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  /* Buttons that lead with an icon: center the glyph against the label. */
  .gdetails__act {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  /* Expand/collapse disclosure: chevron points right, rotates down when the
     neighbors are revealed (neutralized under reduced-motion globally). */
  .gdetails__chev {
    display: inline-flex;
    transition: transform var(--motion-fast) var(--ease);
  }
  .gdetails__chev--open {
    transform: rotate(90deg);
  }
  .pin-badge {
    font-size: 0.72rem;
    color: var(--muted);
  }
</style>
